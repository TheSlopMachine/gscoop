// Tests for severity prefixes (lib/core.ps1:311-314), TTY color with
// NO_COLOR handling, TTY-only progress, and filesize formatting
// (lib/core.ps1:346-363). No emojis.
package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrefixesExact(t *testing.T) {
	var out, errOut bytes.Buffer
	l := &Logger{Out: &out, Err: &errOut}
	l.Error("boom")
	l.Warn("careful")
	l.Info("note")
	l.Debug("hidden")
	if got := errOut.String(); got != "ERROR boom\nWARN  careful\n" {
		t.Fatalf("stderr = %q", got)
	}
	if got := out.String(); got != "INFO  note\n" {
		t.Fatalf("stdout = %q", got)
	}
	l.DebugEnabled = true
	l.Debug("shown")
	if got := out.String(); got != "INFO  note\nDEBUG shown\n" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestColorIdenticalText(t *testing.T) {
	var plain, colored bytes.Buffer
	p := &Logger{Out: &plain, Err: &plain}
	c := &Logger{Out: &colored, Err: &colored, Color: true}
	p.Errorf("boom %d", 1)
	p.Warnf("careful %s", "x")
	p.Infof("note")
	p.Successf("done")
	c.Errorf("boom %d", 1)
	c.Warnf("careful %s", "x")
	c.Infof("note")
	c.Successf("done")
	if StripANSI(colored.String()) != plain.String() {
		t.Fatalf("colored %q strips to non-plain", colored.String())
	}
	if !strings.Contains(colored.String(), "\x1b[") {
		t.Fatal("expected ANSI codes in colored output")
	}
	if strings.Contains(plain.String(), "\x1b[") {
		t.Fatal("plain output must not contain ANSI codes")
	}
}

func TestColorEnabledHonorsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if ColorEnabled(nil, false) {
		t.Fatal("NO_COLOR must disable color")
	}
	if ColorEnabled(nil, true) {
		t.Fatal("flag must disable color")
	}
}

func TestProgressTTYOnly(t *testing.T) {
	var off bytes.Buffer
	p := &Progress{Out: &off}
	p.Update("DL 1/2")
	p.Done()
	if off.Len() != 0 {
		t.Fatalf("disabled progress must stay silent: %q", off.String())
	}
	var on bytes.Buffer
	q := &Progress{Out: &on, Enabled: true}
	q.Update("DL 1/2")
	q.Done()
	got := on.String()
	if !strings.HasPrefix(got, "\rDL 1/2") || !strings.HasSuffix(got, "\n") {
		t.Fatalf("progress = %q", got)
	}
}

func TestFormatSize(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{1024, "1024 B"},
		{1025, "1.0 KB"},
		{1 << 20, "1,024.0 KB"},
		{(1 << 20) + 1, "1.0 MB"},
		{64806912, "61.8 MB"},
		{(1 << 30) + 1, "1.0 GB"},
	}
	for _, c := range cases {
		if got := FormatSize(c.n); got != c.want {
			t.Fatalf("FormatSize(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}
