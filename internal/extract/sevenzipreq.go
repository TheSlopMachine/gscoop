// Archive requirement matching and suffix classification.
//
// TestSevenZipRequirement ports Test-7zipRequirement exactly
// (lib/depends.ps1:134-146), including the trailing (.[^\d.]+)? tail
// and case-insensitive matching. SuffixKind further splits matched
// names into native engines; undecided long-tail suffixes stay on the
// external-7z fallback.
package extract

import (
	"path/filepath"
	"regexp"
	"strings"
)

// sevenZipRequirementRe ports the Test-7zipRequirement pattern
// (lib/depends.ps1:144). PowerShell -match is case-insensitive.
var sevenZipRequirementRe = regexp.MustCompile(`(?i)\.(001|7z|bz(ip)?2?|gz|img|iso|lzma|lzh|nupkg|rar|tar|t[abgpx]z2?|t?zst|xz)(\.[^\d.]+)?$`)

// TestSevenZipRequirement reports whether a URI needs the 7z engine
// (lib/depends.ps1:134-146).
func TestSevenZipRequirement(uri string) bool {
	return sevenZipRequirementRe.MatchString(uri)
}

// SuffixKind names the native engine class for a matched name.
type SuffixKind int

const (
	// SuffixOther is unmatched or long-tail (cab/img/iso/lzma/lzh).
	SuffixOther SuffixKind = iota
	// SuffixSevenZip is .7z and multi-volume .7z.001.
	SuffixSevenZip
	// SuffixRar is .rar including split parts.
	SuffixRar
	// SuffixTar is plain .tar.
	SuffixTar
	// SuffixTarWrapper is compressed tar (.tar.gz, .tgz, .tar.xz,
	// .tar.zst, .tar.lzma, .tar.bz2).
	SuffixTarWrapper
	// SuffixSingle is a lone stream (.gz, .bz2, .xz, .zst).
	SuffixSingle
	// SuffixNupkg is a zip container.
	SuffixNupkg
)

var (
	tarWrapperRe = regexp.MustCompile(`(?i)\.t[abgpx]z2?$`)
	tarSuffixRe  = regexp.MustCompile(`(?i)\.tar(\.(gz|bz2?|bzip2?|xz|lzma|zst))?$`)
	singleRe     = regexp.MustCompile(`(?i)\.(gz|bz2?|bzip2?|xz|zst)$`)
	splitRarRe   = regexp.MustCompile(`(?i)\.part(\d+)\.rar$`)
)

// ClassifySuffix selects a native engine class for an archive name.
func ClassifySuffix(name string) SuffixKind {
	lowered := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lowered, ".nupkg"):
		return SuffixNupkg
	case strings.HasSuffix(lowered, ".rar") || splitRarRe.MatchString(name):
		return SuffixRar
	case tarWrapperRe.MatchString(name) || tarSuffixRe.MatchString(name):
		if tarWrapperRe.MatchString(name) {
			return SuffixTarWrapper
		}
		if strings.HasSuffix(lowered, ".tar") {
			return SuffixTar
		}
		return SuffixTarWrapper
	case singleRe.MatchString(name):
		return SuffixSingle
	case strings.HasSuffix(lowered, ".7z") || IsMultiVolume(name):
		return SuffixSevenZip
	}
	return SuffixOther
}

// IsMultiVolume reports 7z multi-volume first parts (.7z.001), which
// bodgit/sevenzip cannot read (spike-results.md open risk 2).
func IsMultiVolume(name string) bool {
	lowered := strings.ToLower(name)
	if !strings.HasSuffix(lowered, ".001") {
		return false
	}
	return strings.HasSuffix(strings.TrimSuffix(lowered, ".001"), ".7z")
}

// IsSplitRarFirst reports first parts of split rar sets: .partN.rar
// whose number ends in 1 (lib/decompress.ps1:137-139 matches the same
// shape; the removal trigger checks the trailing digit).
func IsSplitRarFirst(name string) bool {
	m := splitRarRe.FindStringSubmatch(name)
	if m == nil {
		return false
	}
	return strings.HasSuffix(m[1], "1")
}

// SplitRarBase strips the .partN.rar suffix, giving the set stem.
func SplitRarBase(name string) (string, bool) {
	if !splitRarRe.MatchString(name) {
		return "", false
	}
	return splitRarRe.ReplaceAllString(name, ""), true
}

// ArchiveExt returns the lowercased final extension of a file name,
// mirroring the extension checks in Expand-7zipArchive removal
// (lib/decompress.ps1:134).
func ArchiveExt(name string) string {
	return strings.ToLower(filepath.Ext(name))
}
