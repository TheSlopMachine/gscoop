package version

import "testing"

func TestCompareEqual(t *testing.T) {
	for _, v := range []string{"1.0", "2.47.0", "nightly", "1.0-alpha", ""} {
		if got := Compare(v, v); got != 0 {
			t.Errorf("Compare(%q, %q) = %d, want 0", v, v, got)
		}
	}
}

func TestComparePlusNormalized(t *testing.T) {
	if got := Compare("1.0+build", "1.0-build"); got != 0 {
		t.Errorf("plus/dash normalization: got %d, want 0", got)
	}
}

func TestCompareNumeric(t *testing.T) {
	cases := []struct {
		ref, diff string
		want      int
	}{
		{"1.0", "1.1", 1},
		{"1.1", "1.0", -1},
		{"1.10", "1.9", -1},
		{"1.9", "1.10", 1},
		{"2.47.0", "2.47.1", 1},
		{"10", "9", -1},
		{"9", "10", 1},
	}
	for _, c := range cases {
		if got := Compare(c.ref, c.diff); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.ref, c.diff, got, c.want)
		}
	}
}

func TestComparePrereleaseSuffix(t *testing.T) {
	cases := []struct {
		ref, diff string
		want      int
	}{
		{"1.1", "1.1-alpha", -1},
		{"1.1-alpha", "1.1", 1},
		{"1.1", "1.1-beta", -1},
		{"1.1-rc1", "1.1", 1},
		{"1.1", "1.1.1", 1},
		{"1.1.1", "1.1", -1},
	}
	for _, c := range cases {
		if got := Compare(c.ref, c.diff); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.ref, c.diff, got, c.want)
		}
	}
}

func TestCompareUnderscoreDelimiter(t *testing.T) {
	if got := Compare("1_2", "1_3"); got != 1 {
		t.Errorf("underscore recursion: got %d, want 1", got)
	}
	if got := CompareWithDelimiter("a_b", "a_c", "_"); got != 1 {
		t.Errorf("explicit underscore delimiter: got %d, want 1", got)
	}
}

func TestCompareNightly(t *testing.T) {
	if got := Compare("nightly", "nightly-20240101"); got != 0 {
		t.Errorf("dual nightly without flag: got %d, want 0", got)
	}
	if got := CompareWithOptions("nightly-20240101", "nightly-20240201", "-", true); got != 1 {
		t.Errorf("nightly date order: got %d, want 1", got)
	}
	if got := CompareWithOptions("nightly-20240201", "nightly-20240101", "-", true); got != -1 {
		t.Errorf("nightly date order reversed: got %d, want -1", got)
	}
	if got := CompareWithOptions("nightly-20240101", "nightly-20240101", "-", true); got != 0 {
		t.Errorf("identical nightly with flag: got %d, want 0", got)
	}
}

func TestSplitVersion(t *testing.T) {
	// SplitVersion with "-" keeps "1.1" whole; dot-splitting happens in
	// Compare recursion (lib/versions.ps1:188-196).
	got := SplitVersion("1.1-alpha", "-")
	want := []any{"1.1", "alpha"}
	if len(got) != len(want) {
		t.Fatalf("SplitVersion length = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("part %d = %v (%T), want %v", i, got[i], got[i], want[i])
		}
	}
}

func TestLatest(t *testing.T) {
	versions := []string{"1.9", "1.10", "1.9.1", "2.0-alpha", "2.0"}
	if got := Latest(versions, "-", false); got != "2.0" {
		t.Errorf("Latest = %q, want %q", got, "2.0")
	}
	if got := Latest(nil, "-", false); got != "" {
		t.Errorf("Latest(nil) = %q, want empty", got)
	}
}
