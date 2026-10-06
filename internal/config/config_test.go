// Tests for case-insensitive config semantics (lib/core.ps1:118-160),
// scoop config get/set/rm display (libexec/scoop-config.ps1:166-191), and
// environment precedence (lib/core.ps1:1347-1385). No emojis.
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaseInsensitiveLookup(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("Proxy", "http://localhost:8080"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"proxy", "PROXY", "Proxy", "pRoXy"} {
		got, ok := s.GetString(name)
		if !ok || got != "http://localhost:8080" {
			t.Fatalf("Get(%q) = %q, %v", name, got, ok)
		}
	}
	if keys := s.Keys(); len(keys) != 1 || keys[0] != "proxy" {
		t.Fatalf("keys = %#v", keys)
	}
}

func TestBoolCoercion(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), "config.json"))
	for _, text := range []string{"True", "true", "TRUE", "False", "false", "FALSE"} {
		if err := s.Set("flag", text); err != nil {
			t.Fatal(err)
		}
		want := strings.EqualFold(text, "true")
		got, ok := s.GetBool("FLAG")
		if !ok || got != want {
			t.Fatalf("Set(%q): GetBool = %v, %v", text, got, ok)
		}
	}
}

func TestSetNilRemoves(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), "config.json"))
	if err := s.Set("debug", true); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("DEBUG", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("debug"); ok {
		t.Fatal("nil Set should remove the key")
	}
	if s.Remove("debug") {
		t.Fatal("second Remove should report missing")
	}
}

func TestUnknownKeysTolerated(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), "config.json"))
	if err := s.Set("GSCOOP_CHANNEL", "nightly"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("totally_custom", "yes"); err != nil {
		t.Fatal(err)
	}
	if got, ok := s.GetString("gscoop_channel"); !ok || got != "nightly" {
		t.Fatalf("got %q, %v", got, ok)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.json")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	values := []struct {
		key   string
		value any
	}{
		{"proxy", "none"},
		{"debug", true},
		{"aria2-split", float64(5)},
		{"private_hosts", []any{map[string]any{"match": "example.com"}}},
		{"gscoop_channel", "stable"},
	}
	for _, v := range values {
		if err := s.Set(v.key, v.value); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.HasPrefix(text, "\xef\xbb\xbf") {
		t.Fatal("BOM written")
	}
	if !strings.HasSuffix(text, "\n") {
		t.Fatal("not newline-terminated")
	}
	for i, line := range strings.Split(text, "\n") {
		if strings.HasSuffix(line, " ") || strings.HasSuffix(line, "\t") {
			t.Fatalf("trailing whitespace on line %d", i+1)
		}
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Len() != len(values) {
		t.Fatalf("len = %d, want %d", loaded.Len(), len(values))
	}
	for i, v := range values {
		if loaded.Keys()[i] != v.key {
			t.Fatalf("key order drift: %#v", loaded.Keys())
		}
	}
	if got, _ := loaded.GetString("PROXY"); got != "none" {
		t.Fatalf("proxy = %q", got)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Len() != 0 {
		t.Fatalf("len = %d", s.Len())
	}
}

func TestLoadMalformedErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadToleratesBOMAndCRLF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := "\xef\xbb\xbf{\r\n  \"PROXY\": \"none\"\r\n}\r\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetString("proxy"); got != "none" {
		t.Fatalf("proxy = %q", got)
	}
}

func TestDisplaySemantics(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), "config.json"))
	if _, ok := s.Display("missing"); ok {
		t.Fatal("missing key should report not set")
	}
	if err := s.Set("hold_update_until", "2026-10-07T00:00:00.0000000+02:00"); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Display("HOLD_UPDATE_UNTIL")
	if !ok {
		t.Fatal("expected value")
	}
	if !strings.HasPrefix(got, "2026-10-0") || !strings.Contains(got, "T") {
		t.Fatalf("date display = %q", got)
	}
	if err := s.Set("debug", true); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Display("debug"); got != "True" {
		t.Fatalf("bool display = %q", got)
	}
}

func TestRootPrecedence(t *testing.T) {
	if got := ResolveScoopDir(`D:\env`, `D:\cfg`, `C:\home`); got != `D:\env` {
		t.Fatalf("got %q", got)
	}
	if got := ResolveScoopDir("", `D:\cfg`, `C:\home`); got != `D:\cfg` {
		t.Fatalf("got %q", got)
	}
	if got := ResolveScoopDir("", "", `C:\home`); got != filepath.Join(`C:\home`, "scoop") {
		t.Fatalf("got %q", got)
	}
	if got := ResolveGlobalDir("", "", `C:\ProgramData`); got != filepath.Join(`C:\ProgramData`, "scoop") {
		t.Fatalf("got %q", got)
	}
	if got := ResolveCacheDir("", "", `D:\scoop`); got != filepath.Join(`D:\scoop`, "cache") {
		t.Fatalf("got %q", got)
	}
	if got := ResolveCacheDir(`D:\cache`, "", `D:\scoop`); got != `D:\cache` {
		t.Fatalf("got %q", got)
	}
}

func TestConfigFilePathPrecedence(t *testing.T) {
	exists := func(p string) bool { return p == `D:\scoop\config.json` }
	got := ConfigFilePathFor(`D:\scoop\apps\scoop\current\bin\scoop.ps1`, "", `C:\home`, exists)
	if got != `D:\scoop\config.json` {
		t.Fatalf("portable should win: %q", got)
	}
	got = ConfigFilePathFor(`D:\other\gscoop.exe`, `D:\xdg`, `C:\home`, func(string) bool { return false })
	if got != filepath.Join(`D:\xdg`, "scoop", "config.json") {
		t.Fatalf("xdg should win: %q", got)
	}
	got = ConfigFilePathFor("", "", `C:\home`, func(string) bool { return false })
	if got != filepath.Join(`C:\home`, ".config", "scoop", "config.json") {
		t.Fatalf("home fallback: %q", got)
	}
}

func TestGitHubTokenPrecedence(t *testing.T) {
	got := ResolveGitHubToken("scoop", "cfg", "gh", "github")
	if got != "scoop" {
		t.Fatalf("got %q", got)
	}
	got = ResolveGitHubToken("", "cfg", "gh", "github")
	if got != "cfg" {
		t.Fatalf("got %q", got)
	}
	got = ResolveGitHubToken("", "", "", "github")
	if got != "github" {
		t.Fatalf("got %q", got)
	}
}

func TestDebugEnabled(t *testing.T) {
	if !DebugEnabled(true, "") {
		t.Fatal("bool true should enable")
	}
	if !DebugEnabled("True", "") {
		t.Fatal("string True should enable")
	}
	if !DebugEnabled(false, "TRUE") {
		t.Fatal("env TRUE should enable")
	}
	if DebugEnabled(false, "") {
		t.Fatal("both unset should disable")
	}
}

func TestGoOnlyDefaults(t *testing.T) {
	defs := Defaults()
	want := map[string]any{
		"max_downloads":    4,
		"split_downloads":  1,
		"shallow_buckets":  false,
		"use_external_git": false,
		"pshost":           "powershell.exe",
		"gscoop_channel":   "stable",
	}
	for k, v := range want {
		if defs[k] != v {
			t.Fatalf("default %s = %#v, want %#v", k, defs[k], v)
		}
	}
}
