// Tests for rar stubs, MSI layout, Inno/Dark guidance, and the
// external-7z path. No installer executes. No emojis.
package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractRarNeedsDependency(t *testing.T) {
	dir := t.TempDir()
	files := map[string][]byte{
		"hello.txt": []byte("rar stored fixture\n"),
	}
	archive := filepath.Join(dir, "a.rar")
	writeFile(t, archive, buildRar15(t, files))
	dest := filepath.Join(dir, "out")
	done, err := Extract(archive, dest, Options{})
	if err != nil || !done {
		t.Fatalf("Extract = %v, %v", done, err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "hello.txt"))
	if err != nil || string(got) != string(files["hello.txt"]) {
		t.Fatalf("hello.txt = %q, %v", got, err)
	}
	if RarModule != "github.com/nwaples/rardecode" {
		t.Errorf("module = %q", RarModule)
	}
}

func TestSplitRarAssembly(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"d.part1.rar", "d.part2.rar"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	parts, err := SplitRarParts(filepath.Join(dir, "d.part1.rar"))
	if err != nil || len(parts) != 2 {
		t.Fatalf("parts = %v, %v", parts, err)
	}
	out := filepath.Join(dir, "joined.bin")
	if err := AssembleSplitRar(parts, out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil || string(got) != "d.part1.rard.part2.rar" {
		t.Errorf("assembled = %q, %v", got, err)
	}
	if _, err := SplitRarParts(filepath.Join(dir, "missing.part1.rar")); err == nil {
		t.Error("missing set must error")
	}
}

func TestMsiAdminArgs(t *testing.T) {
	args := MsiAdminArgs(`C:\cache\app.msi`, `C:\app\1.0`)
	want := []string{"/a", `C:\cache\app.msi`, "/qn", `TARGETDIR=C:\app\1.0\SourceDir`}
	if len(args) != len(want) {
		t.Fatalf("args = %v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args = %v, want %v", args, want)
		}
	}
}

func TestApplyMsiLayout(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "cache", "app.msi")
	work := filepath.Join(dir, "1.0")
	if err := os.MkdirAll(filepath.Join(work, "SourceDir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "SourceDir", "tool.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Stray archive copy beside the output drops on layout.
	if err := os.WriteFile(filepath.Join(work, "app.msi"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMsiLayout(archive, work, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(work, "tool.exe")); err != nil {
		t.Errorf("tool.exe missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, "app.msi")); !os.IsNotExist(err) {
		t.Error("stray archive copy must drop")
	}
	if _, err := os.Stat(filepath.Join(work, "SourceDir")); !os.IsNotExist(err) {
		t.Error("SourceDir must move up")
	}

	scoped := filepath.Join(dir, "scoped_tmp")
	ori := filepath.Join(dir, "scoped")
	if err := os.MkdirAll(filepath.Join(scoped, "SourceDir", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scoped, "SourceDir", "pkg", "t.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMsiLayout(archive, scoped, ori, "pkg"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ori, "t.exe")); err != nil {
		t.Errorf("scoped file missing: %v", err)
	}
	if _, err := os.Stat(scoped); !os.IsNotExist(err) {
		t.Error("staging must be gone")
	}
}

func TestInnoAndDarkGuidance(t *testing.T) {
	if err := InnoError("setup.exe"); err == nil || !strings.Contains(err.Error(), "innounp") {
		t.Errorf("inno guidance = %v", err)
	}
	if InnoHelperName != "innounp" {
		t.Errorf("helper = %q", InnoHelperName)
	}
	if err := DarkError("bundle.exe"); err == nil || !strings.Contains(err.Error(), "dark") {
		t.Errorf("dark guidance = %v", err)
	}
	dir := t.TempDir()
	archive := filepath.Join(dir, "setup.exe")
	if err := os.WriteFile(archive, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Extract(archive, filepath.Join(dir, "out"), Options{InnoSetup: true}); err == nil {
		t.Error("innosetup exe must fail with guidance")
	}
	if done, err := Extract(archive, filepath.Join(dir, "out"), Options{}); err != nil || done {
		t.Errorf("plain exe skips extraction: %v, %v", done, err)
	}
}

func TestExternal7ZipArgs(t *testing.T) {
	args := External7ZipArgs("a.7z", `C:\out`, "")
	joined := strings.Join(args, " ")
	for _, want := range []string{"x", "-xr!*.nsis", "-y", "-oC:\\out"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q miss %q", joined, want)
		}
	}
	scoped := External7ZipArgs("a.zip", `C:\out`, "pkg")
	if !strings.Contains(strings.Join(scoped, " "), `-ir!pkg\*`) {
		t.Errorf("scoped args = %v", scoped)
	}
	tarred := External7ZipArgs("a.tar.gz", `C:\out`, "pkg")
	if strings.Contains(strings.Join(tarred, " "), "-ir!") {
		t.Errorf("tar wrapper must not scope: %v", tarred)
	}
}

func TestExternal7ZipMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := ExtractExternal7Zip("a.7z", "out", "")
	if err == nil || !strings.Contains(err.Error(), "use_external_7zip") {
		t.Errorf("missing 7z = %v", err)
	}
}

func TestExtractAllPairing(t *testing.T) {
	dir := t.TempDir()
	writeZip(t, filepath.Join(dir, "a.zip"), map[string]string{"a.txt": "a"})
	writeZip(t, filepath.Join(dir, "b.zip"), map[string]string{"b.txt": "b"})
	if err := os.WriteFile(filepath.Join(dir, "setup.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The plain exe between archives must not shift b.zip's
	// extract_to: pairing counts extractions only
	// (lib/decompress.ps1:49-58).
	files := []File{
		{Name: "a.zip", ExtractTo: ""},
		{Name: "setup.exe", ExtractTo: ""},
		{Name: "b.zip", ExtractTo: "sub"},
	}
	count, err := ExtractAll(dir, files, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("extracted = %d, want 2", count)
	}
	for _, p := range []string{filepath.Join(dir, "a.txt"), filepath.Join(dir, "sub", "b.txt")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("paired file %s missing: %v", p, err)
		}
	}
}

func TestExtractUnknownSkips(t *testing.T) {
	dir := t.TempDir()
	payload := filepath.Join(dir, "driver.dll")
	if err := os.WriteFile(payload, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	done, err := Extract(payload, filepath.Join(dir, "out"), Options{})
	if err != nil || done {
		t.Errorf("single-file payload skips: %v, %v", done, err)
	}
}
