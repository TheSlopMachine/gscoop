package lnk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "app.exe")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	icon := filepath.Join(dir, "icon.ico")
	if err := os.WriteFile(icon, []byte("i"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Shortcut{Target: target, Name: `tools\demo`, Args: "--serve", Icon: icon}
	path := PathFor(Folder(dir), s.Name)
	if err := Create(path, s); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Target != target || got.Args != "--serve" || got.Icon != icon {
		t.Fatalf("round trip = %+v", got)
	}
	if got.WorkingDir != filepath.Dir(target) {
		t.Fatalf("workdir = %q", got.WorkingDir)
	}
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("link remains")
	}
	// Second remove is a no-op.
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
}

func TestMissingTarget(t *testing.T) {
	s := Shortcut{Target: filepath.Join(t.TempDir(), "missing.exe"), Name: "demo"}
	if err := Create(filepath.Join(t.TempDir(), "demo.lnk"), s); err == nil {
		t.Fatal("want missing-target error")
	}
}

func TestDecodeForeign(t *testing.T) {
	if _, err := Decode([]byte("short")); err == nil {
		t.Fatal("want error for foreign bytes")
	}
}
