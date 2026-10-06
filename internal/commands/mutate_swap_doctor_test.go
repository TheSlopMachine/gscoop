package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnsPhase2B(t *testing.T) {
	for _, name := range []string{"install", "uninstall", "reset", "download", "import", "bucket", "hold", "unhold"} {
		if !OwnsPhase2B(name) {
			t.Errorf("OwnsPhase2B(%q) = false", name)
		}
	}
	for _, name := range []string{"update", "list", "doctor", "unswap"} {
		if OwnsPhase2B(name) {
			t.Errorf("OwnsPhase2B(%q) = true", name)
		}
	}
}

func TestRunPhase2BUnknown(t *testing.T) {
	if code, ok := RunPhase2B(fixtureEnv(t), &bytes.Buffer{}, "update", nil); ok || code != 0 {
		t.Fatalf("RunPhase2B(update) = %d, %v", code, ok)
	}
}

func TestRunBucketKnown(t *testing.T) {
	var buf bytes.Buffer
	if code, ok := RunPhase2B(fixtureEnv(t), &buf, "bucket", []string{"known"}); !ok || code != 0 {
		t.Fatalf("bucket known = %d, %v", code, ok)
	}
	if !strings.Contains(buf.String(), "main") {
		t.Fatalf("bucket known missing main:\n%s", buf.String())
	}
}

func TestRunBucketBadSubcommand(t *testing.T) {
	var buf bytes.Buffer
	if code, _ := RunPhase2B(fixtureEnv(t), &buf, "bucket", []string{"frobnicate"}); code != 1 {
		t.Fatalf("bucket frobnicate = %d", code)
	}
}

func TestRunBucketAddUnknown(t *testing.T) {
	var buf bytes.Buffer
	if code, _ := RunPhase2B(fixtureEnv(t), &buf, "bucket", []string{"add", "nope-unknown"}); code != 1 {
		t.Fatalf("bucket add unknown = %d", code)
	}
}

func TestRunBucketListEmpty(t *testing.T) {
	env := fixtureEnv(t)
	if err := os.RemoveAll(filepath.Join(env.ScoopDir, "buckets")); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if code, _ := RunPhase2B(env, &buf, "bucket", []string{"list"}); code != 2 {
		t.Fatalf("bucket list empty = %d\n%s", code, buf.String())
	}
}

func TestRunUnswapRestores(t *testing.T) {
	env := fixtureEnv(t)
	dir := env.ShimDir(false)
	writeFixture(t, filepath.Join(dir, "scoop.exe"), "gscoop")
	writeFixture(t, filepath.Join(dir, "scoop.shim"), "path = \"gscoop\"\n")
	writeFixture(t, filepath.Join(dir, "scoop.classic.exe"), "classic")
	writeFixture(t, filepath.Join(dir, "scoop.classic.shim"), "path = \"classic\"\n")
	var buf bytes.Buffer
	if code := RunUnswap(env, &buf, nil); code != 0 {
		t.Fatalf("unswap = %d\n%s", code, buf.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, "scoop.exe"))
	if err != nil || string(data) != "classic" {
		t.Fatalf("scoop.exe not restored: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "scoop.classic.exe")); !os.IsNotExist(err) {
		t.Fatal("backup not consumed")
	}
}

func TestRunUnswapNoBackup(t *testing.T) {
	env := fixtureEnv(t)
	var buf bytes.Buffer
	if code := RunUnswap(env, &buf, nil); code != 1 {
		t.Fatalf("unswap without backup = %d", code)
	}
}

func TestRunDoctorExitsZero(t *testing.T) {
	var buf bytes.Buffer
	if code := RunDoctor(fixtureEnv(t), &buf, nil); code != 0 {
		t.Fatalf("doctor = %d\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "problems") && !strings.Contains(buf.String(), "No problems") {
		t.Fatalf("doctor missing summary:\n%s", buf.String())
	}
}
