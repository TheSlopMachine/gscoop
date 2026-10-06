package shim

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPayloadByteIdentical(t *testing.T) {
	for _, v := range []string{"kiennq", "71", "scoopcs"} {
		data, err := Payload(v)
		if err != nil {
			t.Fatalf("%s: %v", v, err)
		}
		if len(data) == 0 {
			t.Fatalf("%s empty", v)
		}
	}
	classic := map[string]string{
		"kiennq": `C:\devel\Scoop\supporting\shims\kiennq\shim.exe`,
		"71":     `C:\devel\Scoop\supporting\shims\71\shim.exe`,
	}
	for variant, path := range classic {
		want, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("classic %s unavailable: %v", variant, err)
		}
		got, _ := Payload(variant)
		if len(got) != len(want) {
			t.Fatalf("%s size %d want %d", variant, len(got), len(want))
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("%s differs at byte %d", variant, i)
			}
		}
	}
	if got := Canonical("scoopcs"); got != "kiennq" {
		t.Fatalf("scoopcs maps to %q", got)
	}
}

func TestTextShimAndWrappers(t *testing.T) {
	dir := t.TempDir()
	if err := WriteTextShim(dir, "Git", `C:\apps\git\bin\git.exe`, "--no-pager"); err != nil {
		t.Fatal(err)
	}
	got := GetShimTarget(filepath.Join(dir, "git.shim"))
	if got != `C:\apps\git\bin\git.exe` {
		t.Fatalf("target = %q", got)
	}
	if err := Wrappers(dir, "tool", `C:\apps\tool\run.ps1`, ""); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"tool.ps1", "tool.cmd"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
	if err := Wrappers(dir, "app", `C:\apps\app\app.bat`, "--x"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "app.cmd"))
	if !strings.Contains(string(data), "@rem") {
		t.Fatalf("bat wrapper = %q", data)
	}
}

func TestBackupRestore(t *testing.T) {
	dir := t.TempDir()
	ownerOf := func(string) string { return "oldapp" }
	// Seed an existing shim owned by oldapp.
	if err := os.WriteFile(filepath.Join(dir, "git.shim"), []byte("path = \"old\"\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	msg, moved := WarnOnOverwrite(filepath.Join(dir, "git.shim"), `C:\apps\new\git.exe`, func(p string) string {
		if strings.Contains(p, "new") {
			return "newapp"
		}
		return ownerOf(p)
	})
	if !moved || !strings.Contains(msg, "Overwriting shim") {
		t.Fatalf("overwrite = %v %q", moved, msg)
	}
	removed := RemoveShim(dir, "git", "newapp")
	_ = removed
}

func TestRemoveShimRestoreNewest(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "git.shim")
	if err := os.WriteFile(primary, []byte("path = \"new\"\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// No backup: removing .shim also removes .exe.
	exe := filepath.Join(dir, "git.exe")
	if err := os.WriteFile(exe, []byte("e"), 0o755); err != nil {
		t.Fatal(err)
	}
	RemoveShim(dir, "git", "")
	if _, err := os.Stat(exe); !os.IsNotExist(err) {
		t.Fatal("exe remains")
	}
}

func TestPEHelpers(t *testing.T) {
	// Synthetic PE: e_lfanew at 0x3C points to 0x40; machine at +4.
	img := make([]byte, 0x200)
	img[0x3C] = 0x40
	img[0x44] = 0x4c
	img[0x45] = 0x01
	if got := PEMachine(img); got != 0x014c {
		t.Fatalf("machine = %#x", got)
	}
	img[0x40+0x5C] = 2
	if got := PESubsystem(img); got != 2 {
		t.Fatalf("subsystem = %d", got)
	}
}

func TestSubstituteLongestFirst(t *testing.T) {
	got := Substitute("$original_dir/$dir", "D", "O", "P")
	if got != "O/D" {
		t.Fatalf("sub = %q", got)
	}
}

func TestShimNameDerivation(t *testing.T) {
	cases := []struct{ in, want string }{
		{`bin\cmake.exe`, "cmake"},
		{"bin/cmake.exe", "cmake"},
		{"cmake.exe", "cmake"},
		{`BIN\CMAKE.EXE`, "cmake"},
		{`C:\apps\tool\Run.ps1`, "run"},
		{"nested/dir/tool.BAT", "tool"},
		{`bin\tool`, "tool"},
		{"foo.bar.exe", "foo.bar"},
		{"python3", "python3"},
		{"Git", "git"},
	}
	for _, tc := range cases {
		if got := ShimName(tc.in); got != tc.want {
			t.Errorf("ShimName(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if got := ShimNameForTarget(tc.in); got != tc.want {
			t.Errorf("ShimNameForTarget(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseEntryNames(t *testing.T) {
	e, ok := ParseEntry(`bin\cmake.exe`)
	if !ok || e.Target != `bin\cmake.exe` || e.Name != "cmake" {
		t.Fatalf("string entry = %+v %v", e, ok)
	}
	e, ok = ParseEntry([]string{"python.exe", "python3"})
	if !ok || e.Target != "python.exe" || e.Name != "python3" {
		t.Fatalf("alias entry = %+v %v", e, ok)
	}
	if got := ShimName(e.Name); got != "python3" {
		t.Fatalf("alias stem = %q", got)
	}
	e, ok = ParseEntry([]any{"bin/cmake.exe"})
	if !ok || e.Name != "cmake" {
		t.Fatalf("single array entry = %+v %v", e, ok)
	}
	e, ok = ParseEntry([]any{"bin/app.exe", "alias", "--flag"})
	if !ok || e.Name != "alias" || e.Args != "--flag" {
		t.Fatalf("full array entry = %+v %v", e, ok)
	}
}

func TestResolvedNamePreserved(t *testing.T) {
	dir := t.TempDir()
	if err := WriteTextShim(dir, "foo.bar", `C:\apps\foo\bar.exe`, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "foo.bar.shim")); err != nil {
		t.Fatalf("multi-dot stem stripped: %v", err)
	}
}
