package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHasWildcard(t *testing.T) {
	cases := []struct {
		spec string
		want bool
	}{
		{"*", true},
		{"?", true},
		{"a?b", true},
		{"a*b", true},
		{"a[bc]", true},
		{"main/*", true},
		{"main/git@1.0.0", false},
		{"git", false},
		{"main/git", false},
		{"git@1.0.0", false},
		{"*@1.0.0", true},
		{"/", true},
		{"bucket/", true},
		{"https://example.com/foo.zip?bar=1", false},
	}
	for _, tc := range cases {
		if got := hasWildcard(tc.spec); got != tc.want {
			t.Errorf("hasWildcard(%q) = %v, want %v", tc.spec, got, tc.want)
		}
	}
}

func TestFindManifestWildcardNeverEmptyName(t *testing.T) {
	env := fixtureEnv(t)
	for _, spec := range []string{"*", "?", "a[bc]", "/", "main/*"} {
		hit := env.FindManifest(spec, &bytes.Buffer{})
		if hit == nil || hit.Manifest != nil {
			t.Fatalf("FindManifest(%q) must miss", spec)
		}
		if hit.Name == "" {
			t.Fatalf("FindManifest(%q) Name must not be empty", spec)
		}
	}
}

func TestInstallWildcardNotFoundNoDir(t *testing.T) {
	prev := mutateDownloader
	mutateDownloader = stubDownloader{}
	t.Cleanup(func() { mutateDownloader = prev })
	env := fixtureEnv(t)
	var buf bytes.Buffer
	if code := RunInstall(env, &buf, []string{"*"}); code != 1 {
		t.Fatalf("install * exit = %d, want 1\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "Couldn't find manifest for '*'") {
		t.Fatalf("install * missing not-found line:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "Installing") {
		t.Fatalf("install * must fail before Installing line:\n%s", buf.String())
	}
	if _, err := os.Stat(filepath.Join(env.ScoopDir, "apps", "*")); err == nil {
		t.Fatal("install * must not create a '*' app dir")
	}
}

func TestInfoWildcardNotFound(t *testing.T) {
	env := fixtureEnv(t)
	for _, spec := range []string{"*", "/"} {
		out, code := runCommand(env, "info", []string{spec})
		if code != 1 {
			t.Fatalf("info %q exit = %d, want 1\n%s", spec, code, out)
		}
		if !strings.Contains(out, "manifest") {
			t.Fatalf("info %q missing manifest notice:\n%s", spec, out)
		}
	}
}

func TestUninstallHoldDownloadWildcardExitOne(t *testing.T) {
	prev := mutateDownloader
	mutateDownloader = stubDownloader{}
	t.Cleanup(func() { mutateDownloader = prev })
	env := fixtureEnv(t)
	var buf bytes.Buffer
	if code := RunUninstall(env, &buf, []string{"*"}); code != 1 {
		t.Fatalf("uninstall * exit = %d, want 1\n%s", code, buf.String())
	}
	buf.Reset()
	if code := RunHold(env, &buf, []string{"*"}, false); code != 1 {
		t.Fatalf("hold * exit = %d, want 1\n%s", code, buf.String())
	}
	buf.Reset()
	if code := RunDownload(env, &buf, []string{"*"}); code != 1 {
		t.Fatalf("download * exit = %d, want 1\n%s", code, buf.String())
	}
}

func TestLiteralPathsWildcardExitOne(t *testing.T) {
	prevOpen := openBrowser
	openBrowser = func(url string) error { return nil }
	t.Cleanup(func() { openBrowser = prevOpen })
	env := fixtureEnv(t)
	if out, code := runCommand(env, "cat", []string{"*"}); code != 1 || !strings.Contains(out, "Couldn't find manifest for '*'") {
		t.Fatalf("cat * exit = %d, out = %q", code, out)
	}
	if out, code := runCommand(env, "prefix", []string{"*"}); code != 1 {
		t.Fatalf("prefix * exit = %d, want 1, out = %q", code, out)
	}
	if out, code := runCommand(env, "depends", []string{"*"}); code != 1 || !strings.Contains(out, "Couldn't find manifest for '*'") {
		t.Fatalf("depends * exit = %d, out = %q", code, out)
	}
	var buf bytes.Buffer
	if code := RunHome(env, &buf, []string{"*"}); code != 1 {
		t.Fatalf("home * exit = %d, want 1, out = %q", code, buf.String())
	}
}
