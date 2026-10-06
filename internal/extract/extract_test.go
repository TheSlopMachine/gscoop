// Tests for dispatch: TestSevenZipRequirement parity and Decide routing
// against testdata/extract/dispatch-cases.json. Classic basis:
// lib/depends.ps1:134-146, lib/decompress.ps1:24-48. No emojis.
package extract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type dispatchCases struct {
	Requirement []struct {
		URI  string `json:"uri"`
		Want bool   `json:"want"`
	} `json:"requirement"`
	Decide []struct {
		Name     string `json:"name"`
		Inno     bool   `json:"inno"`
		External bool   `json:"external"`
		Want     string `json:"want"`
	} `json:"decide"`
}

func loadDispatchCases(t *testing.T) dispatchCases {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "testdata", "extract", "dispatch-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases dispatchCases
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

func formatName(f Format) string {
	switch f {
	case FormatZip:
		return "zip"
	case FormatTar:
		return "tar"
	case FormatSingle:
		return "single"
	case FormatSevenZip:
		return "sevenzip"
	case FormatRar:
		return "rar"
	case FormatMsi:
		return "msi"
	case FormatInno:
		return "inno"
	case FormatISO:
		return "iso"
	case FormatDark:
		return "dark"
	default:
		return "none"
	}
}

func TestSevenZipRequirementVectors(t *testing.T) {
	for _, c := range loadDispatchCases(t).Requirement {
		if got := TestSevenZipRequirement(c.URI); got != c.Want {
			t.Errorf("TestSevenZipRequirement(%q) = %v, want %v", c.URI, got, c.Want)
		}
	}
}

func TestDecide(t *testing.T) {
	for _, c := range loadDispatchCases(t).Decide {
		got := formatName(Decide(c.Name, c.Inno, c.External))
		if got != c.Want {
			t.Errorf("Decide(%q, inno=%v, ext=%v) = %s, want %s", c.Name, c.Inno, c.External, got, c.Want)
		}
	}
}

func TestMultiVolumeAndSplit(t *testing.T) {
	if !IsMultiVolume("backup.7z.001") || IsMultiVolume("backup.001") || IsMultiVolume("backup.7z") {
		t.Error("IsMultiVolume mismatch")
	}
	if !IsSplitRarFirst("data.part1.rar") || !IsSplitRarFirst("data.part01.rar") || IsSplitRarFirst("data.part2.rar") {
		t.Error("IsSplitRarFirst mismatch")
	}
	if base, ok := SplitRarBase("data.part1.rar"); !ok || base != "data" {
		t.Errorf("SplitRarBase = %q, %v", base, ok)
	}
	if _, ok := SplitRarBase("data.rar"); ok {
		t.Error("SplitRarBase must reject plain rar")
	}
}

func TestRemoveArchive(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "a.zip")
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveArchive(plain); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plain); !os.IsNotExist(err) {
		t.Error("plain archive must be removed")
	}
	for _, name := range []string{"b.7z.001", "b.7z.002", "b.7z.003"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveArchive(filepath.Join(dir, "b.7z.001")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"b.7z.001", "b.7z.002", "b.7z.003"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("split sibling %s must be removed", name)
		}
	}
	for _, name := range []string{"c.part1.rar", "c.part2.rar", "c.part3.rar"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveArchive(filepath.Join(dir, "c.part1.rar")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"c.part1.rar", "c.part2.rar", "c.part3.rar"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("rar sibling %s must be removed", name)
		}
	}
}

func TestJoinSecure(t *testing.T) {
	dir := t.TempDir()
	if _, err := JoinSecure(dir, "../evil.txt"); err == nil {
		t.Error(".. escape must fail")
	}
	if _, err := JoinSecure(dir, "a/../../evil.txt"); err == nil {
		t.Error("nested escape must fail")
	}
	got, err := JoinSecure(dir, "sub/file.txt")
	if err != nil || got != filepath.Join(dir, "sub", "file.txt") {
		t.Errorf("JoinSecure = %q, %v", got, err)
	}
	// A rooted name without a drive extracts relatively, like 7z.
	rel, err := JoinSecure(dir, "/abs.txt")
	if err != nil || rel != filepath.Join(dir, "abs.txt") {
		t.Errorf("rooted JoinSecure = %q, %v", rel, err)
	}
	if _, err := JoinSecure(dir, "C:/evil.txt"); err == nil {
		t.Error("drive-letter path must fail")
	}
}

func TestMoveDir(t *testing.T) {
	dir := t.TempDir()
	from := filepath.Join(dir, "from")
	to := filepath.Join(dir, "to")
	if err := os.MkdirAll(filepath.Join(from, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(from, "sub", "f.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Pre-existing to/sub/old.txt proves merging.
	if err := os.MkdirAll(filepath.Join(to, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(to, "sub", "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MoveDir(from, to); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(from); !os.IsNotExist(err) {
		t.Error("source must be gone")
	}
	for _, p := range []string{filepath.Join(to, "sub", "f.txt"), filepath.Join(to, "sub", "old.txt")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("merged file %s missing: %v", p, err)
		}
	}
}

func TestApplyExtractDir(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "out_tmp")
	if err := os.MkdirAll(filepath.Join(tmp, "pkg", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "pkg", "bin", "app.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out")
	if err := ApplyExtractDir(tmp, dest, `pkg`); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "app.exe")); err != nil {
		t.Errorf("moved file missing: %v", err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("staging must be gone")
	}
}
