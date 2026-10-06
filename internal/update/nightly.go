package update

import (
	"strings"
	"time"

	"gscoop/internal/version"
)

// NightlyDated maps nightly to nightly-yyyyMMdd, mirroring
// nightly_version (lib/install.ps1:1-6).
func NightlyDated(now time.Time) string {
	return "nightly-" + now.Format("20060102")
}

// IsNightly reports whether version is nightly or a dated nightly.
func IsNightly(v string) bool {
	return v == "nightly" || strings.HasPrefix(v, "nightly-")
}

// Outdated mirrors the app_status outdated decision
// (lib/core.ps1:589-596): FORCE_UPDATE compares with inequality,
// otherwise Compare-Version greater-than decides. updateNightly
// mirrors UPDATE_NIGHTLY for dual-nightly date comparison.
func Outdated(current, latest string, forceUpdate, updateNightly bool) bool {
	if current == "" || latest == "" {
		return false
	}
	cmp := version.CompareWithOptions(current, latest, "-", updateNightly)
	if forceUpdate {
		return cmp != 0
	}
	return cmp > 0
}

// ResolveTargetVersion maps a manifest version to its install version:
// nightly becomes dated (hash checks switch off at the call site),
// everything else passes through.
func ResolveTargetVersion(manifestVersion string, now time.Time) (target string, nightly bool) {
	if manifestVersion == "nightly" {
		return NightlyDated(now), true
	}
	return manifestVersion, IsNightly(manifestVersion)
}
