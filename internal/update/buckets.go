package update

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/TheSlopMachine/gscoop/internal/gitengine"
)

// BucketResult records one synced bucket: HEAD before and after plus
// the log and diff rows collected for the SQLite refresh path.
type BucketResult struct {
	Name    string
	Before  string
	After   string
	Log     []gitengine.LogEntry
	Diff    []gitengine.DiffEntry
	Skipped bool
}

// BucketReport aggregates per-bucket outcomes plus per-bucket errors.
// One bucket failure never stops the remaining buckets.
type BucketReport struct {
	Buckets []BucketResult
	Errors  map[string]error
}

// chorePattern mirrors Invoke-GitLog grep=^(chore) with invert-grep:
// chore commits stay out of the update log.
var chorePattern = regexp.MustCompile(`^(chore)`)

// SyncBuckets pulls every git-backed bucket. Work fans out over a
// small pool (classic uses throttle limit 5, libexec/scoop-update.ps1:
// 187-189); each repository pull stays serialized through its own
// engine call. Non-git buckets report as skipped. showLog collects
// per-bucket commit rows for display and the diff rows feed the
// SQLite refresh. Failures land in Errors by bucket name.
func SyncBuckets(engine gitengine.GitEngine, bucketsDir string, names []string, showLog bool, emit func(string)) BucketReport {
	report := BucketReport{Errors: map[string]error{}}
	if len(names) == 0 {
		return report
	}
	const pool = 5
	jobs := make(chan string)
	results := make(chan BucketResult, len(names))
	errs := make(chan struct {
		name string
		err  error
	}, len(names))
	var wg sync.WaitGroup
	for w := 0; w < pool && w < len(names); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range jobs {
				res, err := syncOneBucket(engine, bucketsDir, name, showLog)
				if err != nil {
					errs <- struct {
						name string
						err  error
					}{name, err}
					continue
				}
				results <- res
			}
		}()
	}
	for _, n := range names {
		jobs <- n
	}
	close(jobs)
	wg.Wait()
	close(results)
	close(errs)
	var out []BucketResult
	for res := range results {
		out = append(out, res)
	}
	for e := range errs {
		report.Errors[e.name] = e.err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	report.Buckets = out
	if emit != nil {
		for _, b := range out {
			if b.Skipped {
				emit(fmt.Sprintf("'%s' is not a git repository. Skipped.", b.Name))
			}
		}
		for name, err := range report.Errors {
			emit(fmt.Sprintf("Failed to update bucket '%s': %s", name, err.Error()))
		}
	}
	return report
}

// syncOneBucket records HEAD, pulls, then collects the log and diff
// rows used by the update log and the SQLite refresh
// (libexec/scoop-update.ps1:197-252).
func syncOneBucket(engine gitengine.GitEngine, bucketsDir, name string, showLog bool) (BucketResult, error) {
	res := BucketResult{Name: name}
	root := filepath.Join(bucketsDir, name)
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		res.Skipped = true
		return res, nil
	}
	before, err := engine.Head(root)
	if err != nil {
		return res, err
	}
	res.Before = before
	if err := engine.Pull(root, gitengine.PullOptions{}); err != nil {
		return res, err
	}
	after, err := engine.Head(root)
	if err != nil {
		return res, err
	}
	res.After = after
	if showLog && before != "" && before != after {
		entries, err := engine.LogSince(root, after, "", chorePattern, 0)
		if err == nil {
			for _, e := range entries {
				if strings.HasPrefix(e.Hash, before) || e.Hash == before {
					break
				}
				res.Log = append(res.Log, e)
			}
		}
	}
	if before != "" && after != "" && before != after {
		if diff, err := engine.DiffNameStatus(root, before, after); err == nil {
			res.Diff = diff
		}
	}
	return res, nil
}

// EnsureMainGit converts a non-git main bucket through remove plus
// re-add (libexec/scoop-update.ps1:161-171). knownRepo is the registry
// URL for main. A git-backed or absent main needs nothing.
func EnsureMainGit(engine gitengine.GitEngine, bucketsDir, knownRepo string, emit func(string)) error {
	root := filepath.Join(bucketsDir, "main")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		return nil
	}
	if emit != nil {
		emit("Converting 'main' bucket to git repo...")
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("Failed to remove local 'main' bucket.")
	}
	if knownRepo == "" {
		return fmt.Errorf("Failed to add remote 'main' bucket.")
	}
	if _, err := engine.LsRemote(knownRepo); err != nil {
		_ = os.RemoveAll(root)
		return fmt.Errorf("Failed to add remote 'main' bucket.")
	}
	if err := engine.Clone(knownRepo, root, gitengine.CloneOptions{}); err != nil {
		_ = os.RemoveAll(root)
		return fmt.Errorf("Failed to add remote 'main' bucket.")
	}
	return nil
}
