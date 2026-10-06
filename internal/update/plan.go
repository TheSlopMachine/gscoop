package update

import (
	"sort"
	"strings"
)

// Target is one installed app selected for update.
type Target struct {
	App     string
	Global  bool
	Current string
	Latest  string
	Pin     string
}

// StatusView carries the per-app facts the planner needs. Callers in
// internal/commands build it from AppStatusFor plus install metadata,
// keeping this package free of command-local types.
type StatusView struct {
	App           string
	Global        bool
	Installed     bool
	Failed        bool
	Hold          bool
	Version       string
	LatestVersion string
	PinURL        string
	PinBucket     string
}

// SelectTargets mirrors the outdated loop in scoop-update.ps1:437-462.
// Explicit pins on force re-resolve against HEAD instead of guessing.
// Held apps report through heldOut; up-to-date explicit apps report
// through currentOut; missing apps report through missingOut.
// forceUpdate mirrors FORCE_UPDATE, updateNightly mirrors
// UPDATE_NIGHTLY, all covers '*' and --all.
func SelectTargets(states []StatusView, force, forceUpdate, updateNightly, all bool, heldOut, currentOut, missingOut func(StatusView)) []Target {
	var outdated []Target
	for _, st := range states {
		if !st.Installed {
			if missingOut != nil {
				missingOut(st)
			}
			continue
		}
		// A -f pin against a user manifest upgrades to bucket HEAD;
		// the caller resolves the real target version.
		isPin := force && st.PinBucket == "" && st.PinURL != ""
		if (force || Outdated(st.Version, st.LatestVersion, forceUpdate, updateNightly)) && !isPin {
			if st.Hold {
				if heldOut != nil {
					heldOut(st)
				}
				continue
			}
			outdated = append(outdated, Target{App: st.App, Global: st.Global, Current: st.Version, Latest: st.LatestVersion})
			continue
		}
		if isPin {
			outdated = append(outdated, Target{App: st.App, Global: st.Global, Current: st.Version, Latest: "HEAD (forced)", Pin: "head"})
			continue
		}
		if !all {
			if currentOut != nil {
				currentOut(st)
			}
		}
	}
	return outdated
}

// SortTargets orders (app, global) tuples deterministically for the
// sequential install phase.
func SortTargets(in []Target) []Target {
	out := append([]Target(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].App != out[j].App {
			return strings.ToLower(out[i].App) < strings.ToLower(out[j].App)
		}
		return !out[i].Global && out[j].Global
	})
	return out
}
