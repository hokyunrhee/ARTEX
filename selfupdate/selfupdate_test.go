package selfupdate

import (
	"archive/zip"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// testPaths creates an isolated upgrade directory. Do not call ResolvePaths() directly: that would point to
// the test binary itself and rename the go test executable during the test.
func testPaths(t *testing.T) Paths {
	t.Helper()
	dir := t.TempDir()
	return Paths{
		Dir:     dir,
		Current: filepath.Join(dir, "artex"),
		New:     filepath.Join(dir, "artex.new"),
		Sum:     filepath.Join(dir, "artex.new.sha256"),
		Old:     filepath.Join(dir, "artex.old"),
		Marker:  filepath.Join(dir, "artex.upgrade.json"),
	}
}

// fakeBin writes an executable shell script to stand in for artex. smokeTest only invokes -h and checks its exit code,
// so a script is sufficient and much faster than compiling a real binary.
func fakeBin(t *testing.T, path, marker string, exitCode int) {
	t.Helper()
	script := "#!/bin/sh\necho " + marker + "\nexit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("Write fake binary %s: %v", path, err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	return string(rune('0' + n))
}

// stage prepares bin for pending replacement by writing artex.new and its checksum.
func stage(t *testing.T, p Paths, marker string, exitCode int) {
	t.Helper()
	fakeBin(t, p.New, marker, exitCode)
	sum, err := fileSHA256(p.New)
	if err != nil {
		t.Fatalf("Compute checksum: %v", err)
	}
	if err := os.WriteFile(p.Sum, []byte(sum), 0o644); err != nil {
		t.Fatalf("Write checksum: %v", err)
	}
}

func readAll(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Read %s: %v", path, err)
	}
	return string(b)
}

func requireUnix(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("The fake binary is a sh script and cannot run on Windows")
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b       string
		want       int
		comparable bool
	}{
		{"0.3.7", "0.3.8", -1, true},
		{"0.3.8", "0.3.7", 1, true},
		{"0.3.7", "0.3.7", 0, true},
		{"v0.3.7", "0.3.8", -1, true}, // build.sh strips v while tags include it; support both.
		{"0.3.7", "v0.3.7", 0, true},
		{"0.9.0", "0.10.0", -1, true}, // Compare numerically, not lexicographically.
		{"1.0.0", "0.99.99", 1, true},
		// Development builds must be incomparable so releases cannot overwrite uncommitted changes.
		{"dev", "0.3.8", 0, false},
		{"0.3.7-2-gabc1234", "0.3.8", 0, false},
		{"0.3.7-dirty", "0.3.8", 0, false},
		{"0.3", "0.3.8", 0, false},
		{"", "0.3.8", 0, false},
	}
	for _, c := range cases {
		got, ok := CompareVersions(c.a, c.b)
		if ok != c.comparable {
			t.Errorf("CompareVersions(%q,%q) comparable=%v, want %v", c.a, c.b, ok, c.comparable)
			continue
		}
		if ok && got != c.want {
			t.Errorf("CompareVersions(%q,%q)=%d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestResolvePathsNaming(t *testing.T) {
	p, err := ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	// Key invariant: all upgrade files share the executable directory. Using CWD would break replacement for services
	// whose working directory could be /.
	for name, path := range map[string]string{"New": p.New, "Sum": p.Sum, "Old": p.Old, "Marker": p.Marker} {
		if filepath.Dir(path) != p.Dir {
			t.Errorf("%s is outside the executable directory: %s (want %s)", name, path, p.Dir)
		}
	}
	// On Windows, .new/.old must retain .exe for smoke tests and execution after replacement.
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(p.New, ".exe") || !strings.HasSuffix(p.Old, ".exe") {
			t.Errorf("On Windows, .new/.old must end in .exe: new=%s old=%s", p.New, p.Old)
		}
	}
}

func TestVerifyStagedRejectsTamperedBinary(t *testing.T) {
	requireUnix(t)
	p := testPaths(t)
	stage(t, p, "new", 0)

	// Modify the file after writing its checksum to simulate download corruption or tampering.
	fakeBin(t, p.New, "tampered", 0)
	if err := verifyStaged(p); err == nil {
		t.Fatal("Expected SHA256 mismatch to be rejected, but it passed")
	}
}

func TestVerifyStagedRejectsUnrunnableBinary(t *testing.T) {
	requireUnix(t)
	p := testPaths(t)
	stage(t, p, "broken", 1) // Executable, but exits with a nonzero code.

	if err := verifyStaged(p); err == nil {
		t.Fatal("Expected a failed smoke test to be rejected, but it passed")
	}
}

func TestApplyStagedHappyPath(t *testing.T) {
	requireUnix(t)
	p := testPaths(t)
	fakeBin(t, p.Current, "old", 0)
	stage(t, p, "new", 0)
	if err := writeMarker(p.Marker, marker{From: "0.3.7", To: "0.3.8"}); err != nil {
		t.Fatalf("Write marker: %v", err)
	}

	action, st := applyStaged(p)
	if action != Restart {
		t.Fatalf("Expected Restart, got %v", action)
	}
	if !st.Pending {
		t.Error("State should be Pending after replacement")
	}
	if !strings.Contains(readAll(t, p.Current), "new") {
		t.Error("artex should have been replaced with the new version")
	}
	if !strings.Contains(readAll(t, p.Old), "old") {
		t.Error("The previous version should be backed up as artex.old")
	}
	if _, err := os.Stat(p.New); !os.IsNotExist(err) {
		t.Error("artex.new should be gone after replacement")
	}
	if _, err := os.Stat(p.Sum); !os.IsNotExist(err) {
		t.Error("The checksum file should be removed after replacement")
	}
	// Keep the marker: the next startup runs the new version and uses it to count attempts and roll back if needed.
	if _, ok := readMarker(p.Marker); !ok {
		t.Error("The upgrade marker should remain after replacement")
	}
}

func TestApplyStagedKeepsCurrentWhenVerifyFails(t *testing.T) {
	requireUnix(t)
	p := testPaths(t)
	fakeBin(t, p.Current, "old", 0)
	stage(t, p, "new", 0)
	fakeBin(t, p.New, "tampered", 0) // Invalidate the checksum.

	action, st := applyStaged(p)
	if action != Continue {
		t.Fatalf("Expected Continue on verification failure, got %v", action)
	}
	if !st.FailedStage {
		t.Error("State should indicate FailedStage")
	}
	if !strings.Contains(readAll(t, p.Current), "old") {
		t.Fatal("Verification failure must never modify the current version")
	}
	if _, err := os.Stat(p.New); !os.IsNotExist(err) {
		t.Error("Invalid staged files should be removed to avoid another attempt on the next startup")
	}
}

func TestSwapOverwritesPreviousBackup(t *testing.T) {
	requireUnix(t)
	p := testPaths(t)
	fakeBin(t, p.Current, "v2", 0)
	fakeBin(t, p.Old, "v1", 0) // Backup left by the previous upgrade.
	stage(t, p, "v3", 0)

	if err := swap(p); err != nil {
		t.Fatalf("swap: %v", err)
	}
	if !strings.Contains(readAll(t, p.Current), "v3") {
		t.Error("Should replace the current version with v3")
	}
	if !strings.Contains(readAll(t, p.Old), "v2") {
		t.Error("Backup should be updated to the v2 that was just replaced")
	}
}

func TestConfirmCountsAttemptsThenRollsBack(t *testing.T) {
	requireUnix(t)
	p := testPaths(t)
	fakeBin(t, p.Current, "broken-new", 0)
	fakeBin(t, p.Old, "good-old", 0)
	m := marker{From: "0.3.7", To: "0.3.8"}

	// The first maxAttempts starts only increment the count, giving the new version a chance to stabilize.
	for i := 1; i <= maxAttempts; i++ {
		action, st := confirmOrRollback(p, m)
		if action != Continue {
			t.Fatalf("Attempt %d: expected Continue, got %v", i, action)
		}
		if !st.Pending {
			t.Errorf("Attempt %d should have Pending state", i)
		}
		got, ok := readMarker(p.Marker)
		if !ok || got.Attempts != i {
			t.Fatalf("After attempt %d, attempts=%d (ok=%v), want %d", i, got.Attempts, ok, i)
		}
		m = got
	}

	// One more crash exceeds the limit and automatically restores the old version.
	action, st := confirmOrRollback(p, m)
	if action != Restart {
		t.Fatalf("Expected Restart after exceeding the attempt limit, got %v", action)
	}
	if !st.RolledBack {
		t.Error("State should indicate RolledBack")
	}
	if !strings.Contains(readAll(t, p.Current), "good-old") {
		t.Fatal("Should have rolled back to the previous version")
	}
	if _, err := os.Stat(p.Marker); !os.IsNotExist(err) {
		t.Error("The marker should be removed after rollback to prevent an infinite rollback loop")
	}
	// Retain the version that could not start for investigation instead of deleting it.
	if _, err := os.Stat(p.Current + ".failed"); err != nil {
		t.Error("The failed version should remain as .failed for investigation")
	}
}

func TestManualRollbackIsReversible(t *testing.T) {
	requireUnix(t)
	p := testPaths(t)
	fakeBin(t, p.Current, "v2", 0)
	fakeBin(t, p.Old, "v1", 0)

	// Rollback() uses ResolvePaths(); test the underlying swap semantics directly here.
	tmp := p.Current + ".swap"
	if err := os.Rename(p.Current, tmp); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(p.Old, p.Current); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, p.Old); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readAll(t, p.Current), "v1") {
		t.Error("The current version should be v1 after rollback")
	}
	if !strings.Contains(readAll(t, p.Old), "v2") {
		t.Error("After rollback, the backup should become v2 so rollback can be reversed")
	}
}

func TestParseSums(t *testing.T) {
	const (
		linuxSum = "1111111111111111111111111111111111111111111111111111111111111111"
		winSum   = "ABCDEF0000000000000000000000000000000000000000000000000000000000"
	)
	// sha256sum uses two spaces; shasum -a 256 prefixes filenames with * in binary mode.
	raw := linuxSum + "  artex-0.3.8-linux-amd64.zip\n" +
		winSum + " *artex-0.3.8-windows-amd64.zip\n" +
		"\n" +
		"garbage line\n" + // Exactly two fields, but the first is not a digest.
		"deadbeef  artex-0.3.8-darwin-arm64.zip\n" // Incorrect digest length.

	out := parseSums(raw)
	if out["artex-0.3.8-linux-amd64.zip"] != linuxSum {
		t.Errorf("Incorrectly parsed Linux entry: %v", out)
	}
	// Normalize digests to lowercase so case differences do not cause false mismatches.
	if got := out["artex-0.3.8-windows-amd64.zip"]; got != strings.ToLower(winSum) {
		t.Errorf("Incorrect Windows entry (strip the * prefix and lowercase the digest): %q", got)
	}
	if len(out) != 2 {
		t.Errorf("Blank lines, non-digests, and wrong-length lines should be ignored, got %v", out)
	}
}

func TestExtractBinaryFindsNestedEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("The package basename is artex.exe on Windows; this test uses Unix naming")
	}
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "release.zip")

	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	// Actual package layout: artex-<version>-<os>-<arch>/artex, plus unrelated files.
	for name, body := range map[string]string{
		"artex-0.3.8-linux-amd64/README.md":           "readme",
		"artex-0.3.8-linux-amd64/skills/a.md":         "skill",
		"artex-0.3.8-linux-amd64/artex":               "#!/bin/sh\nexit 0\n",
		"artex-0.3.8-linux-amd64/config.example.json": "{}",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	dst := filepath.Join(dir, "out")
	if err := extractBinary(zipPath, dst); err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	if got := readAll(t, dst); !strings.Contains(got, "exit 0") {
		t.Errorf("Extracted content is not the artex executable: %q", got)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Error("The extracted binary must be executable")
	}
}

func TestExtractBinaryMissingEntry(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "release.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("artex-0.3.8-linux-amd64/README.md")
	_, _ = w.Write([]byte("readme"))
	_ = zw.Close()
	f.Close()

	if err := extractBinary(zipPath, filepath.Join(dir, "out")); err == nil {
		t.Fatal("A package without an executable should return an error")
	}
}

func TestCheckURLRejectsNonGitHub(t *testing.T) {
	bad := []string{
		"http://github.com/x",           // Not HTTPS.
		"https://evil.com/artex.zip",    // Domain is not allowlisted.
		"https://github.com.evil.com/x", // Deceptive domain suffix.
		"https://raw.githubusercontent.com.evil.com/x",
	}
	for _, raw := range bad {
		u := mustParse(t, raw)
		if err := checkURL(u); err == nil {
			t.Errorf("checkURL(%q) should reject this URL", raw)
		}
	}
	good := []string{
		"https://api.github.com/repos/x/releases/latest",
		"https://objects.githubusercontent.com/blah",
		"https://GitHub.com/x", // Domain names are case-insensitive.
	}
	for _, raw := range good {
		u := mustParse(t, raw)
		if err := checkURL(u); err != nil {
			t.Errorf("checkURL(%q) should allow this URL, but returned an error: %v", raw, err)
		}
	}
}

func TestAssetNameMatchesBuildScript(t *testing.T) {
	// build.sh's package_binary uses artex-<version>-<os>-<arch>.zip with the v prefix
	// removed. One wrong character here prevents one-click updates from finding assets on every platform.
	if got := AssetName("v0.3.8", "linux", "amd64"); got != "artex-0.3.8-linux-amd64.zip" {
		t.Errorf("AssetName = %q", got)
	}
	if got := AssetName("0.3.8", "windows", "amd64"); got != "artex-0.3.8-windows-amd64.zip" {
		t.Errorf("AssetName = %q", got)
	}
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("Parse %q: %v", raw, err)
	}
	return u
}

func TestSettleClearsMarkerAndStopsRollback(t *testing.T) {
	requireUnix(t)
	p := testPaths(t)
	fakeBin(t, p.Current, "new", 0)
	fakeBin(t, p.Old, "old", 0)
	if err := writeMarker(p.Marker, marker{From: "0.3.7", To: "0.3.8", Attempts: 2}); err != nil {
		t.Fatal(err)
	}

	settle(p)

	if _, err := os.Stat(p.Marker); !os.IsNotExist(err) {
		t.Fatal("The upgrade marker must be removed after stability is confirmed")
	}
	// Without the marker, normal future restarts neither increment attempts nor accidentally trigger rollback.
	if _, ok := readMarker(p.Marker); ok {
		t.Error("Reading the marker should fail")
	}
	// Keep the backup so users can still roll back manually.
	if _, err := os.Stat(p.Old); err != nil {
		t.Error("The previous-version backup should remain after stability is confirmed")
	}
}

func TestSettleIsNoopWithoutMarker(t *testing.T) {
	requireUnix(t)
	p := testPaths(t)
	fakeBin(t, p.Current, "cur", 0)
	settle(p) // Normal startup path: must neither panic nor modify files.
	if _, err := os.Stat(p.Current); err != nil {
		t.Error("Without a marker, settle should leave all files unchanged")
	}
}
