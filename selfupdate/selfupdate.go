// Package selfupdate implements ARTEX's one-click web update: download a new binary
// from GitHub Releases, verify and stage it, then replace the executable atomically on the next startup.
//
// Responsibilities (see start.sh / start.bat):
//
//	Startup script = simple supervisor loop; decides whether to restart the process based on its exit code.
//	This package   = all failure-prone logic (download / SHA256 verification / smoke test / replacement / rollback).
//
// Replacement lives in Go instead of scripts because checksum verification and smoke tests would otherwise require
// two implementations for sh and bat (sha256sum / shasum / certutil), precisely where mistakes are least tolerable:
// a supervisor repeatedly restarts an unusable replacement binary, leaving the user to recover manually on the host.
//
// A complete upgrade spans three process starts:
//
//  1. Old server receives /api/update/apply -> download and verify -> stage artex.new -> exit 75.
//  2. Script restarts old binary -> Bootstrap finds artex.new -> verify and smoke-test -> replace -> exit 75.
//  3. Script starts the new binary -> Bootstrap records one attempt -> clear the marker after successful startup.
//
// Any failed step falls back to the old version: step 2 discards invalid staged files and keeps running the old binary;
// step 3 automatically restores artex.old if three consecutive starts crash before clearing the marker.
package selfupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ExitRestart asks the supervisor to restart the process (EX_TEMPFAIL). Startup scripts restart immediately
// without crash backoff. Exit 0 is a normal user stop (end the loop); all other codes indicate a crash.
const ExitRestart = 75

// maxAttempts limits startup attempts after replacement. Each new-version startup increments the count;
// surviving settleDelay clears the marker. maxAttempts consecutive crashes trigger automatic rollback.
const maxAttempts = 3

// Paths contains every file involved in an upgrade, all under the executable's directory.
// Do not use CWD: services may start from / or any other directory. Using CWD would stage files elsewhere
// and prevent replacement from working.
type Paths struct {
	Dir     string // Executable directory
	Current string // Currently running binary: artex / artex.exe
	New     string // Staged new version: artex.new / artex.new.exe
	Sum     string // New version's SHA256 (hex): artex.new.sha256 / artex.new.exe.sha256
	Old     string // Previous version backed up before replacement: artex.old / artex.old.exe
	Marker  string // Upgrade state marker: artex.upgrade.json
}

// ResolvePaths derives every upgrade path from the current executable.
//
// On Windows, .new/.old files must retain the .exe suffix for smoke tests and execution after replacement.
// Remove the suffix before appending the stage name, keeping names consistent across platforms.
func ResolvePaths() (Paths, error) {
	exe, err := os.Executable()
	if err != nil {
		return Paths{}, fmt.Errorf("Locate executable: %w", err)
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

// marker records replacement progress and triggers automatic rollback when the new version cannot start.
type marker struct {
	From     string `json:"from"`     // Version before the upgrade
	To       string `json:"to"`       // Target version
	Attempts int    `json:"attempts"` // Startup attempts since replacement
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

// cleanStaged removes staged files after successful replacement, failed verification, or cancellation,
// preventing leftover artex.new files from being retried on the next startup.
func cleanStaged(p Paths) {
	_ = os.Remove(p.New)
	_ = os.Remove(p.Sum)
}

// CompareVersions compares two versions, returning -1/0/1 (a<b / a==b / a>b).
// ok=false means at least one value is not a comparable version (for example, a local "dev" build or
// git describe output such as "0.3.7-2-gabc1234-dirty"). Callers should disable one-click updates in this case,
// otherwise an in-development build could be "upgraded" to a release, overwriting uncommitted changes.
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

// parseVersion parses versions such as "v0.3.7" / "0.3.7" into [3]int.
//
// Accept only clean three-part versions: non-tag builds use build.sh's git describe output,
// such as "0.3.7-2-gabc1234". Treat these suffixed versions as incomparable, not as
// 0.3.7, or development builds could be incorrectly reported as up to date or overwritten by releases.
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

// InDocker reports whether the process runs in a container. In Docker, replacement writes to the writable container layer;
// `docker compose up -d` recreates the container from the image's bundled version. This is expected
// (the user is pulling a new image), but the frontend must explain it clearly.
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
