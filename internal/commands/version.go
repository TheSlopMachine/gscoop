package commands

import (
	"fmt"
	"strconv"
	"strings"
)

// CompareVersions ports Compare-Version in lib/versions.ps1 for the
// read-only status and update decisions: 1 when difference is newer than
// reference, -1 when older, 0 when equal.
//
// TODO(version): replace with internal/version when it lands, including the
// UPDATE_NIGHTLY date comparison and property tests against the PowerShell
// oracle. Nightly handling here matches the default (UPDATE_NIGHTLY off).
func CompareVersions(reference, difference string) int {
	ref := strings.ReplaceAll(reference, "+", "-")
	diff := strings.ReplaceAll(difference, "+", "-")
	if diff == ref {
		return 0
	}
	splitRef := splitVersion(ref, '-')
	splitDiff := splitVersion(diff, '-')
	if len(splitRef) > 0 && len(splitDiff) > 0 && splitRef[0] == "nightly" && splitDiff[0] == "nightly" {
		return 0
	}
	max := len(splitRef)
	if len(splitDiff) > max {
		max = len(splitDiff)
	}
	for i := 0; i < max; i++ {
		if i >= len(splitRef) {
			if matchesPre(splitDiff[i]) {
				return -1
			}
			return 1
		}
		if i >= len(splitDiff) {
			if matchesPre(splitRef[i]) {
				return 1
			}
			return -1
		}
		r, d := splitRef[i], splitDiff[i]
		if strings.Contains(r, ".") || strings.Contains(d, ".") {
			if res := CompareVersionsDelim(r, d, '.'); res != 0 {
				return res
			}
			continue
		}
		if strings.Contains(r, "_") || strings.Contains(d, "_") {
			if res := CompareVersionsDelim(r, d, '_'); res != 0 {
				return res
			}
			continue
		}
		rn, rErr := strconv.ParseInt(r, 10, 64)
		dn, dErr := strconv.ParseInt(d, 10, 64)
		if rErr == nil && dErr == nil {
			switch {
			case dn > rn:
				return 1
			case dn < rn:
				return -1
			}
			continue
		}
		// Classic stringifies numbers before comparing against strings.
		rs, ds := r, d
		if rErr == nil {
			rs = strconv.FormatInt(rn, 10)
		}
		if dErr == nil {
			ds = strconv.FormatInt(dn, 10)
		}
		if ds > rs {
			return 1
		}
		if ds < rs {
			return -1
		}
	}
	return 0
}

// CompareVersionsDelim compares with an explicit delimiter, mirroring the
// recursive Compare-Version calls for "." and "_" segments.
func CompareVersionsDelim(reference, difference string, delim rune) int {
	ref := strings.ReplaceAll(reference, "+", "-")
	diff := strings.ReplaceAll(difference, "+", "-")
	if diff == ref {
		return 0
	}
	splitRef := splitVersion(ref, delim)
	splitDiff := splitVersion(diff, delim)
	max := len(splitRef)
	if len(splitDiff) > max {
		max = len(splitDiff)
	}
	for i := 0; i < max; i++ {
		if i >= len(splitRef) {
			if matchesPre(splitDiff[i]) {
				return -1
			}
			return 1
		}
		if i >= len(splitDiff) {
			if matchesPre(splitRef[i]) {
				return 1
			}
			return -1
		}
		rn, rErr := strconv.ParseInt(splitRef[i], 10, 64)
		dn, dErr := strconv.ParseInt(splitDiff[i], 10, 64)
		if rErr == nil && dErr == nil {
			switch {
			case dn > rn:
				return 1
			case dn < rn:
				return -1
			}
			continue
		}
		rs, ds := splitRef[i], splitDiff[i]
		if ds > rs {
			return 1
		}
		if ds < rs {
			return -1
		}
	}
	return 0
}

// splitVersion mirrors SplitVersion in lib/versions.ps1: letter runs are
// wrapped with the delimiter, then the version is split and digit runs are
// kept as strings (numeric comparison happens at compare time).
func splitVersion(version string, delim rune) []string {
	var b strings.Builder
	for _, r := range version {
		if ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') {
			b.WriteRune(delim)
			b.WriteRune(r)
			b.WriteRune(delim)
		} else {
			b.WriteRune(r)
		}
	}
	var out []string
	for _, part := range strings.Split(b.String(), string(delim)) {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func matchesPre(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "alpha") || strings.Contains(l, "beta") ||
		strings.Contains(l, "rc") || strings.Contains(l, "pre")
}

// FormatArch mirrors Format-ArchitectureString in lib/core.ps1.
//
// TODO(config): replace with internal/config when it lands.
func FormatArch(arch string) (string, error) {
	if arch == "" {
		return defaultArch(), nil
	}
	switch strings.ToLower(arch) {
	case "64bit", "64", "x64", "amd64", "x86_64", "x86-64":
		return "64bit", nil
	case "32bit", "32", "x86", "i386", "386", "i686":
		return "32bit", nil
	case "arm64", "arm", "aarch64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("Invalid architecture: '%s'", arch)
	}
}
