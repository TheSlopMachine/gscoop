package hook

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubstitute(t *testing.T) {
	vars := Vars{Dir: `C:\scoop\apps\git\1.0`, OriginalDir: `C:\scoop\apps\git\1.0`, PersistDir: `C:\scoop\persist\git`, Version: "1.0", Global: false}
	got := Substitute(`install --dir "$dir" --persist "$persist_dir" --version $version --global $global`, vars)
	if !strings.Contains(got, vars.Dir) || !strings.Contains(got, vars.PersistDir) || !strings.Contains(got, "1.0") {
		t.Fatalf("substitute missed tokens: %q", got)
	}
	// Longer tokens win over $dir prefix in $original_dir.
	got = Substitute(`$original_dir`, vars)
	if got != vars.OriginalDir {
		t.Fatalf("original_dir mismatch: %q", got)
	}
}

func TestPreambleNoInterpolation(t *testing.T) {
	vars := Vars{Dir: `C:\evil"; Remove-Item C:\`, Version: "1.0"}
	pre := Preamble(vars)
	if strings.Contains(pre, vars.Dir) {
		t.Fatalf("preamble interpolates values: %q", pre)
	}
	for _, want := range []string{"$dir", "$original_dir", "$persist_dir", "$version", "$architecture", "$global", "$SCOOP"} {
		if !strings.Contains(pre, want) {
			t.Fatalf("preamble missing %s: %q", want, pre)
		}
	}
	env := Environ(vars)
	found := false
	for _, e := range env {
		if strings.HasPrefix(e, "GSCOOP_HOOK_DIR=") && strings.Contains(e, vars.Dir) {
			found = true
		}
	}
	if !found {
		t.Fatalf("environ missing dir: %v", env)
	}
}

func TestJoinScript(t *testing.T) {
	if got := JoinScript([]any{"a", "b"}); got != "a\r\nb" {
		t.Fatalf("join = %q", got)
	}
	if got := JoinScript("solo"); got != "solo" {
		t.Fatalf("join = %q", got)
	}
	if got := JoinScript(nil); got != "" {
		t.Fatalf("join = %q", got)
	}
}

func TestRunScriptEmptyNoop(t *testing.T) {
	r := &Runner{Dir: t.TempDir(), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	if err := r.RunScript(context.Background(), TypePreInstall, Vars{}, "   "); err != nil {
		t.Fatal(err)
	}
}

func TestRunScriptUnknownHook(t *testing.T) {
	r := &Runner{Dir: t.TempDir(), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	if err := r.RunScript(context.Background(), "bogus", Vars{}, "echo hi"); err == nil {
		t.Fatal("want error for unknown hook")
	}
}

func TestScriptPathGolden(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "golden")
	vars := Vars{Dir: `C:\scoop\apps\demo\1.0`, Version: "1.0"}
	path, err := ScriptPathForTest(dir, vars, "Write-Host 'hi'")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Write-Host 'hi'") {
		t.Fatalf("golden missing script: %q", data)
	}
}

func TestRunScriptWithPowershell(t *testing.T) {
	host := "powershell.exe"
	if _, err := os.Stat(`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`); err != nil {
		t.Skip("powershell.exe unavailable")
	}
	var out bytes.Buffer
	r := &Runner{PSHost: host, Dir: t.TempDir(), Out: &out, Err: &out}
	vars := Vars{Dir: r.Dir, Version: "9.9"}
	if err := r.RunScript(context.Background(), TypePreInstall, vars, "exit 0"); err != nil {
		t.Fatalf("run: %v out=%q", err, out.String())
	}
	if !strings.Contains(out.String(), "Running pre_install") {
		t.Fatalf("framing missing: %q", out.String())
	}
	var out2 bytes.Buffer
	r2 := &Runner{PSHost: host, Dir: t.TempDir(), Out: &out2, Err: &out2}
	if err := r2.RunScript(context.Background(), TypePreInstall, vars, "exit 3"); err == nil {
		t.Fatal("want non-zero abort")
	}
}
