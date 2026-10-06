package update

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/TheSlopMachine/gscoop/internal/config"
	"github.com/TheSlopMachine/gscoop/internal/gitengine"
)

func testStore(t *testing.T, content string) (*config.Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return store, path
}

func discardWriter() *sliceWriter {
	return &sliceWriter{}
}

type sliceWriter struct {
	lines []string
}

func (w *sliceWriter) Write(p []byte) (int, error) {
	w.lines = append(w.lines, string(p))
	return len(p), nil
}

func TestParseAppSpec(t *testing.T) {
	cases := []struct {
		in          string
		app, bucket string
		version     string
	}{
		{"git", "git", "", ""},
		{"main/git", "git", "main", ""},
		{"gh@2.7.0", "gh", "", "2.7.0"},
		{"extras/tool@1.0", "tool", "extras", "1.0"},
	}
	for _, c := range cases {
		got := ParseAppSpec(c.in)
		if got.App != c.app || got.Bucket != c.bucket || got.Version != c.version {
			t.Errorf("ParseAppSpec(%q) = %+v, want app=%q bucket=%q version=%q", c.in, got, c.app, c.bucket, c.version)
		}
		if ShowApp(got.App, got.Bucket, got.Version) != c.in {
			t.Errorf("ShowApp round trip of %q = %q", c.in, ShowApp(got.App, got.Bucket, got.Version))
		}
	}
}

func TestParseHoldUntil(t *testing.T) {
	now := time.Now()
	future := now.Add(2 * time.Hour).Format(time.RFC3339Nano)
	past := now.Add(-2 * time.Hour).Format(time.RFC3339Nano)
	if state, _ := ParseHoldUntil("", now); state != HoldOff {
		t.Errorf("empty hold = %v, want HoldOff", state)
	}
	if state, _ := ParseHoldUntil(future, now); state != HoldOn {
		t.Errorf("future hold = %v, want HoldOn", state)
	}
	if state, _ := ParseHoldUntil(past, now); state != HoldExpired {
		t.Errorf("past hold = %v, want HoldExpired", state)
	}
	if state, _ := ParseHoldUntil("not-a-date", now); state != HoldInvalid {
		t.Errorf("garbage hold = %v, want HoldInvalid", state)
	}
}

func TestCheckCoreHold(t *testing.T) {
	now := time.Now()
	future := now.Add(24 * time.Hour).Format(time.RFC3339Nano)
	store, _ := testStore(t, `{"hold_update_until": "`+future+`"}`)
	if !CheckCoreHold(store, discardWriter(), now) {
		t.Error("future hold must skip self-update")
	}
	if _, ok := store.Get("hold_update_until"); !ok {
		t.Error("active hold must stay in config")
	}
	past := now.Add(-time.Hour).Format(time.RFC3339Nano)
	store, _ = testStore(t, `{"hold_update_until": "`+past+`"}`)
	if CheckCoreHold(store, discardWriter(), now) {
		t.Error("expired hold must not skip self-update")
	}
	if _, ok := store.Get("hold_update_until"); ok {
		t.Error("expired hold must be cleared")
	}
	store, _ = testStore(t, `{"hold_update_until": "garbage"}`)
	if CheckCoreHold(store, discardWriter(), now) {
		t.Error("invalid hold must not skip self-update")
	}
	if _, ok := store.Get("hold_update_until"); ok {
		t.Error("invalid hold must be cleared")
	}
	store, _ = testStore(t, `{}`)
	if CheckCoreHold(store, discardWriter(), now) {
		t.Error("missing hold must not skip self-update")
	}
}

func TestScoopOutdated(t *testing.T) {
	now := time.Now()
	store, _ := testStore(t, `{}`)
	if !ScoopOutdated(store, now) {
		t.Error("missing LAST_UPDATE must count as stale")
	}
	old := now.Add(-4 * time.Hour).Format(time.RFC3339Nano)
	store, _ = testStore(t, `{"last_update": "`+old+`"}`)
	if !ScoopOutdated(store, now) {
		t.Error("4h-old LAST_UPDATE must count as stale")
	}
	fresh := now.Add(-time.Hour).Format(time.RFC3339Nano)
	store, _ = testStore(t, `{"last_update": "`+fresh+`"}`)
	if ScoopOutdated(store, now) {
		t.Error("1h-old LAST_UPDATE must count as fresh")
	}
}

func TestOutdated(t *testing.T) {
	if !Outdated("1.0", "2.0", false, false) {
		t.Error("2.0 must obsolete 1.0")
	}
	if Outdated("2.0", "1.0", false, false) {
		t.Error("1.0 must not obsolete 2.0")
	}
	if Outdated("1.0", "1.0", false, false) {
		t.Error("equal versions must not obsolete")
	}
	if !Outdated("1.0", "1.0.1", true, false) {
		t.Error("FORCE_UPDATE must obsolete on any difference")
	}
	if Outdated("nightly-20240101", "nightly-20240201", false, false) {
		t.Error("dual nightly must compare equal without UPDATE_NIGHTLY")
	}
	if !Outdated("nightly-20240101", "nightly-20240201", false, true) {
		t.Error("UPDATE_NIGHTLY must date-compare nightly versions")
	}
}

func TestNightlyHelpers(t *testing.T) {
	now := time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC)
	if got := NightlyDated(now); got != "nightly-20240305" {
		t.Errorf("NightlyDated = %q, want nightly-20240305", got)
	}
	if !IsNightly("nightly") || !IsNightly("nightly-20240305") || IsNightly("2.0") {
		t.Error("IsNightly misclassified versions")
	}
	target, nightly := ResolveTargetVersion("nightly", now)
	if !nightly || target != "nightly-20240305" {
		t.Errorf("ResolveTargetVersion(nightly) = %q,%v", target, nightly)
	}
	if target, nightly := ResolveTargetVersion("2.0", now); nightly || target != "2.0" {
		t.Errorf("ResolveTargetVersion(2.0) = %q,%v", target, nightly)
	}
}

func TestSelectTargets(t *testing.T) {
	states := []StatusView{
		{App: "old", Installed: true, Version: "1.0", LatestVersion: "2.0"},
		{App: "held", Installed: true, Hold: true, Version: "1.0", LatestVersion: "2.0"},
		{App: "same", Installed: true, Version: "2.0", LatestVersion: "2.0"},
		{App: "gone", Installed: false},
	}
	var held, current, missing []string
	got := SelectTargets(states, false, false, false, false,
		func(s StatusView) { held = append(held, s.App) },
		func(s StatusView) { current = append(current, s.App) },
		func(s StatusView) { missing = append(missing, s.App) })
	if len(got) != 1 || got[0].App != "old" {
		t.Errorf("SelectTargets = %+v, want only old", got)
	}
	if len(held) != 1 || held[0] != "held" {
		t.Errorf("held = %v, want [held]", held)
	}
	if len(current) != 1 || current[0] != "same" {
		t.Errorf("current = %v, want [same]", current)
	}
	if len(missing) != 1 || missing[0] != "gone" {
		t.Errorf("missing = %v, want [gone]", missing)
	}
	// Force re-resolves URL pins against HEAD.
	pinned := []StatusView{{App: "pin", Installed: true, Version: "1.0", LatestVersion: "1.0", PinURL: "workspace/pin.json"}}
	got = SelectTargets(pinned, true, false, false, false, nil, nil, nil)
	if len(got) != 1 || got[0].Pin != "head" {
		t.Errorf("forced pin = %+v, want HEAD re-resolve", got)
	}
}

func TestCompareSemver(t *testing.T) {
	if CompareSemver("1.0.0", "1.0.1") != 1 {
		t.Error("1.0.1 must be newer than 1.0.0")
	}
	if CompareSemver("2.0.0", "1.9.9") != -1 {
		t.Error("1.9.9 must be older than 2.0.0")
	}
	if CompareSemver("1.0.0", "1.0.0") != 0 {
		t.Error("equal versions must compare 0")
	}
	if CompareSemver("1.0.0", "1.0.0-beta") != -1 {
		t.Error("release must beat pre-release")
	}
	if !ShouldSelfUpdate("1.0.0", "1.0.1", "stable") {
		t.Error("stable must take newer releases")
	}
	if ShouldSelfUpdate("1.0.0", "1.0.1-beta", "stable") {
		t.Error("stable must skip pre-releases")
	}
	if !ShouldSelfUpdate("1.0.0", "1.0.1-beta", "nightly") {
		t.Error("nightly must take newer pre-releases")
	}
	if ShouldSelfUpdate("1.0.1", "1.0.0", "nightly") {
		t.Error("older versions must never self-update")
	}
}

func TestSwapExecutable(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "gscoop.exe")
	next := filepath.Join(dir, "gscoop.new")
	if err := os.WriteFile(current, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(next, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SwapExecutable(current, next); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(current)
	if err != nil || string(data) != "new" {
		t.Errorf("current after swap = %q,%v, want new", data, err)
	}
	data, err = os.ReadFile(current + ".old")
	if err != nil || string(data) != "old" {
		t.Errorf("backup after swap = %q,%v, want old", data, err)
	}
}

// fakeEngine is an in-memory GitEngine for sync, history, and
// self-update tests. No network, no git binary.
type fakeEngine struct {
	heads    map[string]string
	files    map[string]map[string][]byte
	log      map[string][]gitengine.LogEntry
	diff     map[string][]gitengine.DiffEntry
	status   map[string]gitengine.FileStatus
	remotes  map[string]string
	pulled   []string
	cloned   []string
	pullErr  map[string]error
	lsErr    error
	cloneErr error
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{
		heads:   map[string]string{},
		files:   map[string]map[string][]byte{},
		log:     map[string][]gitengine.LogEntry{},
		diff:    map[string][]gitengine.DiffEntry{},
		status:  map[string]gitengine.FileStatus{},
		remotes: map[string]string{},
		pullErr: map[string]error{},
	}
}

func (f *fakeEngine) LsRemote(url string) (string, error) {
	if f.lsErr != nil {
		return "", f.lsErr
	}
	return "abc123", nil
}

func (f *fakeEngine) Clone(url, dir string, opts gitengine.CloneOptions) error {
	if f.cloneErr != nil {
		return f.cloneErr
	}
	f.cloned = append(f.cloned, dir)
	return os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
}

func (f *fakeEngine) Pull(repo string, opts gitengine.PullOptions) error {
	if err, ok := f.pullErr[repo]; ok {
		return err
	}
	f.pulled = append(f.pulled, repo)
	return nil
}

func (f *fakeEngine) Fetch(repo, refspec string, force bool) error { return nil }

func (f *fakeEngine) CheckoutCreate(repo, branch, track string) error { return nil }

func (f *fakeEngine) ResetHard(repo, rev string) error { return nil }

func (f *fakeEngine) Head(repo string) (string, error) { return f.heads[repo], nil }

func (f *fakeEngine) ConfigGet(repo, key string) (string, error) { return f.remotes[repo], nil }

func (f *fakeEngine) ConfigSet(repo, key, value string) error {
	f.remotes[repo+"|"+key] = value
	return nil
}

func (f *fakeEngine) LogSince(repo, rev string, pathFilter string, invertGrep *regexp.Regexp, limit int) ([]gitengine.LogEntry, error) {
	var out []gitengine.LogEntry
	for _, e := range f.log[repo] {
		if invertGrep != nil && invertGrep.MatchString(e.Message) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (f *fakeEngine) DiffNameStatus(repo, a, b string) ([]gitengine.DiffEntry, error) {
	return f.diff[repo], nil
}

func (f *fakeEngine) ShowFile(repo, rev, path string) ([]byte, error) {
	byRev, ok := f.files[repo]
	if !ok {
		return nil, os.ErrNotExist
	}
	raw, ok := byRev[rev+"|"+path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return raw, nil
}

func (f *fakeEngine) Status(repo string) (gitengine.FileStatus, error) {
	return f.status[repo], nil
}

func (f *fakeEngine) StashLike(repo string) (gitengine.StashResult, error) {
	return gitengine.StashResult{}, nil
}
