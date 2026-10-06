// Golden help-text tests: Help(name) must equal testdata/cli/<cmd>.txt.
// Also enforces the style gate on golden files: LF only, newline-terminated,
// no trailing whitespace, no BOM. No emojis.
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandCount(t *testing.T) {
	if len(Commands) != 28 {
		t.Fatalf("got %d commands", len(Commands))
	}
	if len(Names()) != 28 {
		t.Fatalf("got %d names", len(Names()))
	}
}

func TestHelpGoldens(t *testing.T) {
	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			got, ok := Help(name)
			if !ok {
				t.Fatalf("no help for %q", name)
			}
			path := filepath.Join("..", "..", "testdata", "cli", name+".txt")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			if string(raw) != got {
				t.Fatalf("golden mismatch for %q", name)
			}
		})
	}
}

func TestUnknownHelp(t *testing.T) {
	if _, ok := Help("nosuchcommand"); ok {
		t.Fatal("expected false for unknown command")
	}
	if Lookup("nosuchcommand") != nil {
		t.Fatal("expected nil lookup")
	}
}

func TestGoldenStyle(t *testing.T) {
	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "..", "testdata", "cli", name+".txt")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			s := string(raw)
			if strings.HasPrefix(s, "\xef\xbb\xbf") {
				t.Fatal("BOM present")
			}
			if strings.Contains(s, "\r") {
				t.Fatal("CR present; goldens use LF")
			}
			if !strings.HasSuffix(s, "\n") {
				t.Fatal("not newline-terminated")
			}
			for i, line := range strings.Split(s, "\n") {
				if strings.HasSuffix(line, " ") || strings.HasSuffix(line, "\t") {
					t.Fatalf("trailing whitespace on line %d", i+1)
				}
			}
		})
	}
}

func TestIsHelpFlag(t *testing.T) {
	for _, f := range []string{"-h", "--help", "/?"} {
		if !IsHelpFlag(f) {
			t.Fatalf("expected help flag %q", f)
		}
	}
	for _, f := range []string{"-v", "--version", "-g", ""} {
		if IsHelpFlag(f) {
			t.Fatalf("unexpected help flag %q", f)
		}
	}
}
