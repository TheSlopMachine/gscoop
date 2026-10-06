// Property and fuzz tests for the Compare-Version port (lib/versions.ps1).
//
// They assert the parity invariants the classic must satisfy: results stay
// in {-1, 0, 1}, self-comparison is 0, comparison is antisymmetric, and '+'
// normalizes to '-' (lib/versions.ps1:142). Bare-nightly cases pin the
// today-yyyyMMdd default (lib/versions.ps1:157-162) with dates on either side
// of any realistic clock. No emojis.
package version

import (
	"strings"
	"testing"
)

func TestCompareNightlyBareDefaults(t *testing.T) {
	// Bare nightly defaults to today: after 19700101, before 29990101.
	if got := CompareWithOptions("nightly", "nightly-19700101", "-", true); got != -1 {
		t.Fatalf("got %d, want -1", got)
	}
	if got := CompareWithOptions("nightly", "nightly-29990101", "-", true); got != 1 {
		t.Fatalf("got %d, want 1", got)
	}
	if got := CompareWithOptions("nightly", "nightly", "-", true); got != 0 {
		t.Fatalf("got %d, want 0", got)
	}
}

func TestCompareAntisymmetryTable(t *testing.T) {
	versions := []string{
		"1.0", "1.1", "1.0.1", "1.1-alpha", "1.1-beta", "2.0",
		"nightly", "nightly-20240101", "1_2", "1.0+build", "1.0-build",
		"1.01", "v1.2.3",
	}
	for _, a := range versions {
		for _, b := range versions {
			for _, flag := range []bool{false, true} {
				fwd := CompareWithOptions(a, b, "-", flag)
				rev := CompareWithOptions(b, a, "-", flag)
				if fwd < -1 || fwd > 1 || rev < -1 || rev > 1 {
					t.Fatalf("Compare(%q, %q, %v) out of range: %d/%d", a, b, flag, fwd, rev)
				}
				if fwd != -rev {
					t.Fatalf("Compare(%q, %q, %v) = %d, reverse = %d", a, b, flag, fwd, rev)
				}
			}
		}
		if got := Compare(a, a); got != 0 {
			t.Fatalf("reflexivity: Compare(%q, %q) = %d, want 0", a, a, got)
		}
	}
}

// FuzzCompareVersion fuzzes the same invariants over arbitrary inputs.
func FuzzCompareVersion(f *testing.F) {
	seeds := []string{
		"1.0", "1.1", "2.47.0", "nightly", "nightly-20240101",
		"1.1-alpha", "1.1-beta", "1.1-rc1", "1.1-pre", "1_2", "a",
		"1.0+build", "", "10", "1.01", "v1.2.3", "2024.01_rc2",
	}
	for _, a := range seeds {
		for _, b := range seeds {
			f.Add(a, b)
		}
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		for _, flag := range []bool{false, true} {
			fwd := CompareWithOptions(a, b, "-", flag)
			if fwd < -1 || fwd > 1 {
				t.Fatalf("Compare(%q, %q) = %d, want -1/0/1", a, b, fwd)
			}
			if rev := CompareWithOptions(b, a, "-", flag); fwd != -rev {
				t.Fatalf("antisymmetry: Compare(%q, %q) = %d, reverse = %d", a, b, fwd, rev)
			}
			plus := CompareWithOptions(strings.ReplaceAll(a, "+", "-"), b, "-", flag)
			if plus != fwd {
				t.Fatalf("plus normalization: Compare(%q, %q) = %d, want %d", a, b, fwd, plus)
			}
		}
		if got := Compare(a, a); got != 0 {
			t.Fatalf("reflexivity: Compare(%q, %q) = %d, want 0", a, a, got)
		}
	})
}
