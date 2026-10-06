package deps

import (
	"reflect"
	"testing"
)

type fake struct {
	deps    map[string][]string
	buckets map[string]string
}

func (f *fake) Depends(app, _ string) ([]string, error) { return f.deps[app], nil }
func (f *fake) Helpers(_, _ string) []string            { return nil }
func (f *fake) Exists(app string) bool {
	_, ok := f.deps[app]
	return ok
}
func (f *fake) BucketOf(app string) string { return f.buckets[app] }

func TestResolveOrder(t *testing.T) {
	f := &fake{deps: map[string][]string{
		"app":  {"lib", "tool"},
		"lib":  {"base"},
		"tool": {"base"},
		"base": {},
	}}
	got, err := Resolve(f, "app", "64bit")
	if err != nil {
		t.Fatal(err)
	}
	// base before lib and tool, app last.
	pos := map[string]int{}
	for i, v := range got {
		pos[v] = i
	}
	if !(pos["base"] < pos["lib"] && pos["base"] < pos["tool"] && pos["lib"] < pos["app"] && pos["tool"] < pos["app"]) {
		t.Fatalf("order = %v", got)
	}
}

func TestCircular(t *testing.T) {
	f := &fake{deps: map[string][]string{"a": {"b"}, "b": {"a"}}}
	if _, err := Resolve(f, "a", "64bit"); err == nil {
		t.Fatal("want circular error")
	}
}

func TestMissing(t *testing.T) {
	f := &fake{deps: map[string][]string{}}
	if _, err := Resolve(f, "ghost", "64bit"); err == nil {
		t.Fatal("want missing error")
	}
}

func TestHelpersNoop(t *testing.T) {
	if got := InstallationHelpers([]string{"x.zip"}, nil, "64bit"); len(got) != 0 {
		t.Fatalf("helpers = %v", got)
	}
}

func TestResolveAllDedup(t *testing.T) {
	f := &fake{deps: map[string][]string{"a": {"base"}, "b": {"base"}, "base": {}}}
	got, err := ResolveAll(f, []string{"a", "b"}, "64bit")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, v := range got {
		if v == "base" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("dedup = %v", got)
	}
	if !reflect.DeepEqual(got[len(got)-1], "b") {
		t.Fatalf("tail = %v", got)
	}
}
