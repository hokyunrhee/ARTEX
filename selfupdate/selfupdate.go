// Package selfupdate implements ARTEX's one-click in-page update: pull a new binary
// from a GitHub Release, verify it, stage it, and atomically swap it in on the next
// startup.
//
// Division of labor (see start.sh / start.bat):
//
//	start script = a dumb guardian loop; it only decides "after the process exits,
//	               relaunch it or not based on the exit code"
//	this package = all the error-prone logic (download / SHA256 verify / smoke test
//	               / swap / rollback on failure)
//
// The swap lives in Go rather than the script because the sha256 verification and
// smoke test would otherwise each need two implementations, one for sh and one for
// bat (sha256sum / shasum / certutil), and this is exactly the step that must not go
// wrong — swap in a binary that won't run and the guardian will faithfully relaunch
// it over and over, leaving the user to log into the machine and recover by hand.
//
// A full upgrade spans three process starts:
//
//	① the old server receives /api/update/apply → download & verify → stage
//	   artex.new → exit 75
//	② the script relaunches the old version → Bootstrap finds artex.new → verify +
//	   smoke test → swap → exit 75
//	③ the script relaunches, now on the new version → Bootstrap records one attempt →
//	   clears the marker once startup succeeds
//
// Any failed step falls back to the old version: at ② a failed verification deletes
// the staged file and keeps running the old version; at ③ three consecutive failures
// to survive long enough to clear the marker (it crashes before coming up) auto-swap
// artex.old back in.
package selfupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ExitRestart is the "please relaunch me" exit code (EX_TEMPFAIL). When the start
// script sees it, it relaunches immediately and does not count it toward crash
// backoff. 0 means the user stopped normally (the script exits its loop); anything
// else is treated as a crash.
const ExitRestart = 75

// maxAttempts is the number of startup attempts allowed after a swap. Each startup
// of the new version increments the count by 1; surviving past settleDelay clears
// the marker. maxAttempts crashes in a row means the new version simply won't come
// up, so it auto-rolls back.
const maxAttempts = 3

// Paths is every file involved in one upgrade, all anchored under the directory the
// executable lives in. Deliberately not CWD: under a service runner the working
// directory may be / or any path, and using CWD would land the staged file
// elsewhere, breaking the swap logic outright.
type Paths struct {
	Dir     string // directory the executable lives in
	Current string // the currently running binary    artex      / artex.exe
	New     string // the staged new version          artex.new  / artex.new.exe
	Sum     string // the new version's sha256 (hex)  artex.new.sha256 / artex.new.exe.sha256
	Old     string // the old version backed up before the swap  artex.old / artex.old.exe
	Marker  string // the upgrade-state marker        artex.upgrade.json
}

// ResolvePaths derives all the upgrade paths from the current executable.
//
// On Windows the .new/.old files must also carry the .exe suffix, or both the smoke
// test and the post-swap execution fail, so we strip the suffix first and then
// reattach it, keeping the naming symmetric on both platforms.
func ResolvePaths() (Paths, error) {
	exe, err := os.Executable()
	if err != nil {
		return Paths{}, fmt.Errorf("locate executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)
	name := filepath.Base(exe)
	ext := filepath.Ext(name) // ".exe" on Windows, usually empty on Unix
	stem := strings.TrimSuffix(name, ext)

	join := func(suffix string) string { return filepath.Join(dir, stem+suffix+ext) }
	return Paths{
		Dir:     dir,
		Current: exe,
		New:     join(".new"),
		Sum:     join(".new") + ".sha256",
		Old:     join(".old"),
		Marker:  filepath.Join(dir, stem+".upgrade.json"),
	}, nil
}

// marker records the progress of one swap, used to trigger an auto-rollback when
// the new version won't come up.
type marker struct {
	From     string `json:"from"`     // the version before the upgrade
	To       string `json:"to"`       // the target version
	Attempts int    `json:"attempts"` // startup attempts made after the swap
	StagedAt int64  `json:"staged_at"`
}

func readMarker(path string) (marker, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return marker{}, false
	}
	var m marker
	if json.Unmarshal(b, &m) != nil {
		return marker{}, false
	}
	return m, true
}

func writeMarker(path string, m marker) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// cleanStaged removes the staged files. A successful swap, a failed verification,
// and a user cancel all go through it, so a leftover artex.new is not retried on the
// next startup.
func cleanStaged(p Paths) {
	_ = os.Remove(p.New)
	_ = os.Remove(p.Sum)
}

// CompareVersions compares two version numbers, returning -1/0/1 (a<b / a==b / a>b).
// ok=false means at least one side is not a comparable version number (e.g. a local
// dev build's "dev", or git describe's "0.3.7-2-gabc1234-dirty"), in which case the
// caller should disable one-click update — otherwise it would "upgrade" a dev build
// into a release and overwrite uncommitted changes.
func CompareVersions(a, b string) (int, bool) {
	av, aok := parseVersion(a)
	bv, bok := parseVersion(b)
	if !aok || !bok {
		return 0, false
	}
	for i := range 3 {
		if av[i] != bv[i] {
			if av[i] < bv[i] {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

// parseVersion parses a version number of the form "v0.3.7" / "0.3.7" into [3]int.
//
// It accepts only a clean three-part form: on non-tag builds build.sh uses git
// describe to produce suffixed versions like "0.3.7-2-gabc1234", and those must be
// judged non-comparable rather than treated as 0.3.7 — otherwise a dev build would
// be mistaken for "already latest" or be overwritten by a release.
func parseVersion(s string) ([3]int, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return [3]int{}, false
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

// InDocker reports whether the process runs inside a container. Under Docker the
// swap writes to the container's writable layer, and `docker compose up -d`
// rebuilding the container reverts to the version baked into the image — this is
// expected behavior (at that point the user is pulling a new image anyway), but the
// frontend needs to be able to say so clearly.
func InDocker() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	b, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	s := string(b)
	return strings.Contains(s, "docker") || strings.Contains(s, "containerd")
}
