package junction

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateReadDelete(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "1.0.0")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "sentinel.txt"), []byte("payload\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "current")
	if err := Create(link, target); err != nil {
		t.Fatalf("create: %v", err)
	}
	if !IsJunction(link) {
		t.Fatal("link not detected as junction")
	}
	got, err := Target(link)
	if err != nil {
		t.Fatalf("target: %v", err)
	}
	want, _ := filepath.Abs(target)
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("target = %q want %q", got, want)
	}
	data, err := os.ReadFile(filepath.Join(link, "sentinel.txt"))
	if err != nil || string(data) != "payload\n" {
		t.Fatalf("traverse: %v %q", err, data)
	}
	if err := Delete(link); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatal("link remains after delete")
	}
	if _, err := os.Stat(filepath.Join(target, "sentinel.txt")); err != nil {
		t.Fatalf("target removed: %v", err)
	}
}

func TestReplaceRejectsCurrent(t *testing.T) {
	root := t.TempDir()
	if err := Replace(filepath.Join(root, "current"), filepath.Join(root, "current")); err == nil {
		t.Fatal("want error for version current")
	}
}

func TestLinkUnlinkCurrent(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "apps", "demo")
	ver := filepath.Join(appDir, "1.0.0")
	if err := os.MkdirAll(ver, 0o755); err != nil {
		t.Fatal(err)
	}
	var lines []string
	current, err := LinkCurrent(appDir, ver, func(s string) { lines = append(lines, s) })
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("link lines = %v", lines)
	}
	ref := UnlinkCurrent(appDir, ver, nil)
	if ref != current {
		t.Fatalf("ref = %q want %q", ref, current)
	}
	if _, err := os.Lstat(current); !os.IsNotExist(err) {
		t.Fatal("current remains")
	}
}
