package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// smokeEnv makes the child process launched by the smoke test skip Bootstrap immediately.
//
// Omitting it would technically work: the child's os.Executable() is artex.new, so all derived
// paths include the .new prefix and cannot touch the real upgrade files. Relying on that coincidence is fragile;
// an explicit short circuit is clearer and avoids unnecessary disk checks in the child.
const smokeEnv = "ARTEX_SELFUPDATE_SMOKE"

// Action is Bootstrap's instruction to main.
type Action int

const (
	// Continue starts the server normally.
	Continue Action = iota
	// Restart exits immediately with ExitRestart so the supervisor script restarts the process.
	Restart
)

// State describes upgrade status during this startup, allowing /api/update/check to report accurately
// whether the previous upgrade succeeded or was rolled back.
type State struct {
	Pending     bool   // Replaced, but stability not yet confirmed
	RolledBack  bool   // Automatic rollback occurred during this startup
	FailedStage bool   // Staged verification/smoke test failed; staged files discarded
	Detail      string // One-sentence user-facing explanation
}

// Bootstrap runs at the very beginning of main, before binding ports or opening the database.
//
// Three situations:
//
//  1. artex.new exists -> verify and smoke-test; replace and request restart on success, otherwise discard and keep the old version.
//  2. Only the marker remains -> replacement just occurred; count one attempt and roll back after enough consecutive failures.
//  3. Neither exists -> normal startup.
func Bootstrap() (Action, State) {
	if os.Getenv(smokeEnv) != "" {
		return Continue, State{}
	}
	p, err := ResolvePaths()
	if err != nil {
		log.Printf("[update] Bootstrap skipped: %v", err)
		return Continue, State{}
	}

	if _, err := os.Stat(p.New); err == nil {
		return applyStaged(p)
	}

	m, ok := readMarker(p.Marker)
	if !ok {
		return Continue, State{}
	}
	return confirmOrRollback(p, m)
}

// applyStaged handles staged files: replace after successful verification, otherwise discard them.
//
// This is the only point in the update path that replaces the executable, and the final gate: the smoke test catches
// corrupted downloads, wrong architectures, and missing dynamic libraries. If an unusable binary passes,
// the supervisor repeatedly restarts it before any Go code can run, making automatic rollback impossible.
func applyStaged(p Paths) (Action, State) {
	m, _ := readMarker(p.Marker)

	if err := verifyStaged(p); err != nil {
		log.Printf("[update] Staged version failed verification and was discarded; continuing with the current version: %v", err)
		cleanStaged(p)
		_ = os.Remove(p.Marker)
		return Continue, State{FailedStage: true, Detail: "New version failed verification and was discarded: " + err.Error()}
	}

	if err := swap(p); err != nil {
		log.Printf("[update] Replacement failed; continuing with the current version: %v", err)
		cleanStaged(p)
		_ = os.Remove(p.Marker)
		return Continue, State{FailedStage: true, Detail: "Replacement failed: " + err.Error()}
	}

	// Replacement succeeded. Keep the marker so the next startup, running the new version, can confirm stability.
	m.Attempts = 0
	if m.StagedAt == 0 {
		m.StagedAt = time.Now().Unix()
	}
	if err := writeMarker(p.Marker, m); err != nil {
		log.Printf("[update] Failed to write upgrade marker (automatic rollback is unavailable): %v", err)
	}
	log.Printf("[update] Replaced with %s; exiting to restart (exit %d)", orUnknown(m.To), ExitRestart)
	return Restart, State{Pending: true}
}

// confirmOrRollback handles startup after replacement: increment attempts and restore the old version if the limit is exceeded.
//
// The counter increments only after Go code starts, so it covers binaries that run but crash during initialization
// (incompatible configuration, occupied ports, failed DB migrations). The pre-replacement smoke test catches binaries
// that cannot execute at all; both checks are needed for complete coverage.
func confirmOrRollback(p Paths, m marker) (Action, State) {
	m.Attempts++
	if m.Attempts > maxAttempts {
		if err := rollback(p); err != nil {
			// If rollback also fails, stop restarting to avoid an infinite loop. Remove the marker
			// and start the process in its current state; if startup fails, the logs can at least show the cause.
			log.Printf("[update] New version failed to start %d consecutive times, and rollback failed: %v", maxAttempts, err)
			_ = os.Remove(p.Marker)
			return Continue, State{Detail: "New version failed to start and rollback failed: " + err.Error()}
		}
		log.Printf("[update] New version failed to start %d consecutive times; rolled back to %s and exiting to restart (exit %d)",
			maxAttempts, orUnknown(m.From), ExitRestart)
		_ = os.Remove(p.Marker)
		return Restart, State{RolledBack: true, Detail: fmt.Sprintf("New version failed to start; rolled back to %s", orUnknown(m.From))}
	}
	if err := writeMarker(p.Marker, m); err != nil {
		log.Printf("[update] Failed to update upgrade marker: %v", err)
	}
	log.Printf("[update] New version starting (attempt %d/%d); the upgrade will be confirmed after stable operation",
		m.Attempts, maxAttempts)
	return Continue, State{Pending: true}
}

// Settle confirms that the new version is stable and removes the upgrade marker.
//
// main calls it after a delay following HTTP listener startup. Only surviving that interval counts;
// otherwise the marker remains and the next startup continues counting attempts until rollback triggers.
func Settle() {
	p, err := ResolvePaths()
	if err != nil {
		return
	}
	settle(p)
}

func settle(p Paths) {
	if _, ok := readMarker(p.Marker); !ok {
		return // Not a startup after an upgrade; nothing to do.
	}
	if err := os.Remove(p.Marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("[update] Failed to remove upgrade marker: %v", err)
		return
	}
	log.Printf("[update] New version is stable; upgrade complete (previous version retained as %s)", p.Old)
}

// SettleDelay is the runtime required to consider the new version stable.
const SettleDelay = 30 * time.Second

// verifyStaged verifies staged files: compare SHA256, then actually run the binary once.
func verifyStaged(p Paths) error {
	want, err := os.ReadFile(p.Sum)
	if err != nil {
		return fmt.Errorf("Read checksum: %w", err)
	}
	got, err := fileSHA256(p.New)
	if err != nil {
		return fmt.Errorf("Compute checksum: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(string(want)), got) {
		return errors.New("SHA256 mismatch (download corrupted or tampered with)")
	}
	return smokeTest(p.New)
}

// smokeTest launches the new binary with -h to confirm it can execute on this system.
// This catches truncated downloads, wrong architectures (exec format error), missing dependencies, and similar failures.
func smokeTest(bin string) error {
	if err := os.Chmod(bin, 0o755); err != nil {
		return fmt.Errorf("Set executable permissions: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "-h")
	cmd.Env = append(os.Environ(), smokeEnv+"=1")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return errors.New("Smoke test timed out (new binary did not respond)")
	}
	if err != nil {
		snippet := strings.TrimSpace(string(out))
		if len(snippet) > 300 {
			snippet = snippet[:300] + "…"
		}
		return fmt.Errorf("Smoke test failed: %v: %s", err, snippet)
	}
	return nil
}

// swap replaces the current binary with the staged new version.
//
// Unix and Windows both allow renaming a running executable (Windows prohibits deletion and overwriting,
// not rename), so no platform-specific path or prior self-termination is needed here.
func swap(p Paths) error {
	// Windows rename does not overwrite an existing destination; remove the .old left by the previous upgrade first.
	if err := os.Remove(p.Old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("Remove previous backup %s: %w", p.Old, err)
	}
	if err := os.Rename(p.Current, p.Old); err != nil {
		return fmt.Errorf("Back up current version: %w", err)
	}
	if err := os.Rename(p.New, p.Current); err != nil {
		// Replacement failed after moving the current version aside. Restore it exactly, or the next startup will have no executable.
		if rerr := os.Rename(p.Old, p.Current); rerr != nil {
			return fmt.Errorf("Installing the new version failed (%v), and restoring the current version failed: %w", err, rerr)
		}
		return fmt.Errorf("Install new version: %w", err)
	}
	_ = os.Remove(p.Sum)
	return nil
}

// rollback restores the old version backed up by swap.
func rollback(p Paths) error {
	if _, err := os.Stat(p.Old); err != nil {
		return fmt.Errorf("No backup available for rollback at %s: %w", p.Old, err)
	}
	// Move the unusable new version to .failed for investigation instead of deleting it.
	failed := p.Current + ".failed"
	_ = os.Remove(failed)
	if err := os.Rename(p.Current, failed); err != nil {
		return fmt.Errorf("Move failed version aside: %w", err)
	}
	if err := os.Rename(p.Old, p.Current); err != nil {
		return fmt.Errorf("Restore previous version: %w", err)
	}
	return nil
}

// Rollback implements /api/update/rollback: explicitly return to the previous version.
// It only replaces files; the supervisor handles restart (the caller subsequently exits with ExitRestart).
func Rollback() error {
	p, err := ResolvePaths()
	if err != nil {
		return err
	}
	if _, err := os.Stat(p.Old); err != nil {
		return errors.New("No previous version is available for rollback (" + p.Old + " does not exist)")
	}
	cleanStaged(p)
	if err := smokeTest(p.Old); err != nil {
		return fmt.Errorf("The previous version cannot run; rollback refused: %w", err)
	}
	// Swap current and backup versions so this rollback can itself be reversed.
	tmp := p.Current + ".swap"
	_ = os.Remove(tmp)
	if err := os.Rename(p.Current, tmp); err != nil {
		return fmt.Errorf("Move current version aside: %w", err)
	}
	if err := os.Rename(p.Old, p.Current); err != nil {
		_ = os.Rename(tmp, p.Current)
		return fmt.Errorf("Install previous version: %w", err)
	}
	if err := os.Rename(tmp, p.Old); err != nil {
		log.Printf("[update] Failed to organize backup after rollback (operation is unaffected): %v", err)
	}
	_ = os.Remove(p.Marker)
	return nil
}

// HasBackup reports whether a previous version is available, letting the frontend decide whether to show the rollback button.
func HasBackup() bool {
	p, err := ResolvePaths()
	if err != nil {
		return false
	}
	_, err = os.Stat(p.Old)
	return err == nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "Unknown version"
	}
	return s
}
