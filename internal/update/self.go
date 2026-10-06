package update

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gscoop/internal/gitengine"
)

// semverPattern parses major.minor.patch plus optional pre-release.
var semverPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.\-]+))?$`)

// CompareSemver orders dotted versions: 1 when latest is newer,
// -1 when older, 0 when equal. Non-semver input compares
// case-insensitively as a fallback.
func CompareSemver(current, latest string) int {
	cm := semverPattern.FindStringSubmatch(strings.TrimSpace(current))
	lm := semverPattern.FindStringSubmatch(strings.TrimSpace(latest))
	if cm == nil || lm == nil {
		a, b := strings.ToLower(strings.TrimSpace(current)), strings.ToLower(strings.TrimSpace(latest))
		switch {
		case b > a:
			return 1
		case b < a:
			return -1
		default:
			return 0
		}
	}
	for i := 1; i <= 3; i++ {
		c, _ := strconv.Atoi(cm[i])
		l, _ := strconv.Atoi(lm[i])
		switch {
		case l > c:
			return 1
		case l < c:
			return -1
		}
	}
	cPre, lPre := cm[4], lm[4]
	switch {
	case cPre != "" && lPre == "":
		return 1
	case cPre == "" && lPre != "":
		return -1
	case lPre > cPre:
		return 1
	case lPre < cPre:
		return -1
	default:
		return 0
	}
}

// ShouldSelfUpdate gates the binary self-update on the channel
// (GSCOOP_CHANNEL stable|nightly): stable skips pre-release targets,
// nightly takes any newer version.
func ShouldSelfUpdate(current, latest, channel string) bool {
	if CompareSemver(current, latest) <= 0 {
		return false
	}
	if strings.EqualFold(channel, "nightly") {
		return true
	}
	return semverPattern.FindStringSubmatch(strings.TrimSpace(latest))[4] == ""
}

// SwapExecutable replaces the running binary through rename-swap:
// the current file moves to .old, the new file takes its place.
// Rename stays on one volume, which keeps the swap atomic on
// Windows while the old image is memory-mapped.
func SwapExecutable(currentPath, newPath string) error {
	oldPath := currentPath + ".old"
	_ = os.Remove(oldPath)
	if err := os.Rename(currentPath, oldPath); err != nil {
		return fmt.Errorf("backing up current binary: %w", err)
	}
	if err := os.Rename(newPath, currentPath); err != nil {
		_ = os.Rename(oldPath, currentPath)
		return fmt.Errorf("activating new binary: %w", err)
	}
	return nil
}

// FastForwardClassic fast-forwards the classic scoop checkout at
// apps/scoop/current through gitengine. Dirty trees stash through
// the StashLikeTo ladder under AUTOSTASH_ON_CONFLICT and abort
// otherwise. A changed repo or branch reconfigures and resets;
// the steady path pulls with tags and force. Failures return an
// error the caller reports as WARN classic scoop core not updated.
func FastForwardClassic(engine gitengine.GitEngine, coreDir, repo, branch string, autostash bool, workspaceDir string, emit func(string)) error {
	if _, err := os.Stat(filepath.Join(coreDir, ".git")); err != nil {
		return fmt.Errorf("classic scoop checkout has no git metadata")
	}
	status, err := engine.Status(coreDir)
	if err != nil {
		return err
	}
	if status.Dirty {
		if !autostash {
			return fmt.Errorf("uncommitted changes detected")
		}
		res, err := gitengine.StashLikeTo(engine, coreDir, workspaceDir)
		if err != nil {
			return err
		}
		if emit != nil {
			emit(fmt.Sprintf("Uncommitted changes stashed to %s.", res.BackupDir))
		}
	}
	currentRepo, _ := engine.ConfigGet(coreDir, "remote.origin.url")
	changed := currentRepo != "" && repo != "" && !gitengine.SameRemote(currentRepo, repo)
	if changed {
		if err := engine.ConfigSet(coreDir, "remote.origin.url", repo); err != nil {
			return err
		}
		if err := engine.ConfigSet(coreDir, "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
			return err
		}
		if err := engine.Fetch(coreDir, "refs/heads/"+branch+":refs/remotes/origin/"+branch, true); err != nil {
			return err
		}
		if err := engine.CheckoutCreate(coreDir, branch, "origin/"+branch); err != nil {
			return err
		}
		return engine.ResetHard(coreDir, "origin/"+branch)
	}
	return engine.Pull(coreDir, gitengine.PullOptions{Force: true, Tags: true})
}
