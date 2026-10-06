// Unit tests ported from test/Scoop-GetOpts.Tests.ps1, plus parity checks
// for documented getopt edge cases. No emojis.
package cli

import (
	"reflect"
	"testing"
)

func TestShortOptionMissingArgument(t *testing.T) {
	r := GetOpt([]string{"-x"}, "x:", nil)
	if r.Err != "Option -x requires an argument." {
		t.Fatalf("got %q", r.Err)
	}
	r = GetOpt([]string{"-xy"}, "x:y", nil)
	if r.Err != "Option -x requires an argument." {
		t.Fatalf("got %q", r.Err)
	}
}

func TestLongOptionMissingArgument(t *testing.T) {
	r := GetOpt([]string{"--arb"}, "", []string{"arb="})
	if r.Err != "Option --arb requires an argument." {
		t.Fatalf("got %q", r.Err)
	}
}

func TestSpaceInValue(t *testing.T) {
	r := GetOpt([]string{"-x", "space arg"}, "x:", nil)
	if r.Err != "" {
		t.Fatalf("got %q", r.Err)
	}
	if got := r.Get("x"); got != "space arg" {
		t.Fatalf("got %q", got)
	}
}

func TestUnrecognizedShortOption(t *testing.T) {
	r := GetOpt([]string{"-az"}, "a", nil)
	if r.Err != "Option -z not recognized." {
		t.Fatalf("got %q", r.Err)
	}
}

func TestUnrecognizedLongOption(t *testing.T) {
	r := GetOpt([]string{"--non-exist"}, "", nil)
	if r.Err != "Option --non-exist not recognized." {
		t.Fatalf("got %q", r.Err)
	}
	r = GetOpt([]string{"--global", "--another"}, "abc:de:", []string{"global", "one"})
	if r.Err != "Option --another not recognized." {
		t.Fatalf("got %q", r.Err)
	}
}

func TestRemainingArgs(t *testing.T) {
	r := GetOpt([]string{"-g", "rem"}, "g", nil)
	if r.Err != "" {
		t.Fatalf("got %q", r.Err)
	}
	if !r.Has("g") {
		t.Fatal("missing g")
	}
	if !reflect.DeepEqual(r.Rest, []string{"rem"}) {
		t.Fatalf("got %#v", r.Rest)
	}
}

func TestLongFlagAndShortOptionWithArgument(t *testing.T) {
	r := GetOpt([]string{"--global", "-a", "32bit", "test"}, "ga:", []string{"global", "arch="})
	if r.Err != "" {
		t.Fatalf("got %q", r.Err)
	}
	if !r.Has("global") {
		t.Fatal("missing global")
	}
	if got := r.Get("a"); got != "32bit" {
		t.Fatalf("got %q", got)
	}
	if !reflect.DeepEqual(r.Rest, []string{"test"}) {
		t.Fatalf("got %#v", r.Rest)
	}
}

func TestRegexCharactersDoNotThrow(t *testing.T) {
	r := GetOpt([]string{"-?"}, "ga:", []string{"global", "arch="})
	if r.Err != "Option -? not recognized." {
		t.Fatalf("got %q", r.Err)
	}
	r = GetOpt([]string{"-?"}, "?:", []string{"help"})
	if r.Err != "Option -? requires an argument." {
		t.Fatalf("got %q", r.Err)
	}
}

func TestShortFlagWithoutArgument(t *testing.T) {
	r := GetOpt([]string{"-x"}, "x", nil)
	if r.Err != "" {
		t.Fatalf("got %q", r.Err)
	}
	if !r.Has("x") {
		t.Fatal("missing x")
	}
}

func TestLongFlagWithoutArgument(t *testing.T) {
	r := GetOpt([]string{"--long-arg"}, "", []string{"long-arg"})
	if r.Err != "" {
		t.Fatalf("got %q", r.Err)
	}
	if !r.Has("long-arg") {
		t.Fatal("missing long-arg")
	}
}

func TestLongOptionWithArgument(t *testing.T) {
	r := GetOpt([]string{"--long-arg", "test"}, "", []string{"long-arg="})
	if r.Err != "" {
		t.Fatalf("got %q", r.Err)
	}
	if got := r.Get("long-arg"); got != "test" {
		t.Fatalf("got %q", got)
	}
}

func TestTerminatorAlone(t *testing.T) {
	r := GetOpt([]string{"--long-arg", "--"}, "", []string{"long-arg"})
	if r.Err != "" {
		t.Fatalf("got %q", r.Err)
	}
	if !r.Has("long-arg") {
		t.Fatal("missing long-arg")
	}
	if len(r.Rest) != 0 {
		t.Fatalf("got %#v", r.Rest)
	}
}

func TestRemainderAfterTerminator(t *testing.T) {
	r := GetOpt([]string{"--long-arg", "--", "-x", "-y"}, "xy", []string{"long-arg"})
	if r.Err != "" {
		t.Fatalf("got %q", r.Err)
	}
	if !r.Has("long-arg") {
		t.Fatal("missing long-arg")
	}
	if r.Has("x") || r.Has("y") {
		t.Fatalf("terminator leak: %#v", r.Opts)
	}
	if !reflect.DeepEqual(r.Rest, []string{"-x", "-y"}) {
		t.Fatalf("got %#v", r.Rest)
	}
}

func TestStopParsingTokenAlone(t *testing.T) {
	r := GetOpt([]string{"--long-arg", "--%"}, "", []string{"long-arg"})
	if r.Err != "" {
		t.Fatalf("got %q", r.Err)
	}
	if !r.Has("long-arg") {
		t.Fatal("missing long-arg")
	}
	if len(r.Rest) != 0 {
		t.Fatalf("got %#v", r.Rest)
	}
}

func TestRemainderAfterStopParsingToken(t *testing.T) {
	r := GetOpt([]string{"--long-arg", "--%", "--from", "there", "--to", "here"}, "", []string{"long-arg"})
	if r.Err != "" {
		t.Fatalf("got %q", r.Err)
	}
	if r.Has("from") || r.Has("to") {
		t.Fatalf("terminator leak: %#v", r.Opts)
	}
	want := []string{"--from", "there", "--to", "here"}
	if !reflect.DeepEqual(r.Rest, want) {
		t.Fatalf("got %#v", r.Rest)
	}
}

func TestBundledShortFlags(t *testing.T) {
	r := GetOpt([]string{"-gp"}, "gp", []string{"global", "purge"})
	if r.Err != "" {
		t.Fatalf("got %q", r.Err)
	}
	if !r.Has("g") || !r.Has("p") {
		t.Fatalf("got %#v", r.Opts)
	}
}

func TestAttachedShortValueRejected(t *testing.T) {
	// Classic requires the value as the next argument (edge E3).
	r := GetOpt([]string{"-a32bit"}, "a:", []string{"arch="})
	if r.Err != "Option -a requires an argument." {
		t.Fatalf("got %q", r.Err)
	}
}

func TestLongEqualsFormRejected(t *testing.T) {
	// Classic has no "--opt=value" form (edge E2).
	r := GetOpt([]string{"--arch=32bit"}, "a:", []string{"arch="})
	if r.Err != "Option --arch=32bit not recognized." {
		t.Fatalf("got %q", r.Err)
	}
}

func TestLoneDashIsPositional(t *testing.T) {
	r := GetOpt([]string{"-", "app"}, "ga:", []string{"global"})
	if r.Err != "" {
		t.Fatalf("got %q", r.Err)
	}
	if !reflect.DeepEqual(r.Rest, []string{"-", "app"}) {
		t.Fatalf("got %#v", r.Rest)
	}
}

func TestCaseInsensitiveMatch(t *testing.T) {
	// PowerShell -match is case-insensitive (edge E4).
	r := GetOpt([]string{"--GLOBAL", "-G"}, "g", []string{"global"})
	if r.Err != "" {
		t.Fatalf("got %q", r.Err)
	}
	if !r.Has("global") || !r.Has("g") {
		t.Fatalf("got %#v", r.Opts)
	}
}

func TestOptionsAfterPositionalsStillParse(t *testing.T) {
	r := GetOpt([]string{"app", "-g"}, "g", []string{"global"})
	if r.Err != "" {
		t.Fatalf("got %q", r.Err)
	}
	if !r.Has("g") {
		t.Fatal("missing g")
	}
	if !reflect.DeepEqual(r.Rest, []string{"app"}) {
		t.Fatalf("got %#v", r.Rest)
	}
}

func TestLastWinsOnRepeat(t *testing.T) {
	r := GetOpt([]string{"-a", "32bit", "-a", "64bit"}, "a:", []string{"arch="})
	if r.Err != "" {
		t.Fatalf("got %q", r.Err)
	}
	if got := r.Get("a"); got != "64bit" {
		t.Fatalf("got %q", got)
	}
}
