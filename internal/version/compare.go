// Package version ports lib/versions.ps1 Compare-Version.
//
// Compare reports 1 when the difference version exceeds the reference
// version, -1 when it is lower, and 0 when both are equal. Fully equal
// inputs return 0; classic PowerShell returns $null there and every caller
// treats that result as 0. No emojis.
package version

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	letterRun = regexp.MustCompile(`[a-zA-Z]+`)
	digitRun  = regexp.MustCompile(`^\d+$`)
	qualifier = regexp.MustCompile(`(?i)alpha|beta|rc|pre`)
)

// Compare orders reference against difference with the default "-"
// delimiter and nightly versions treated as equal.
func Compare(reference, difference string) int {
	return CompareWithOptions(reference, difference, "-", false)
}

// CompareWithDelimiter orders reference against difference with delimiter.
func CompareWithDelimiter(reference, difference, delimiter string) int {
	return CompareWithOptions(reference, difference, delimiter, false)
}

// CompareWithOptions orders reference against difference.
//
// updateNightly mirrors the UPDATE_NIGHTLY config: dual nightly versions
// compare by embedded yyyyMMdd date instead of reporting equal.
func CompareWithOptions(reference, difference, delimiter string, updateNightly bool) int {
	reference = strings.ReplaceAll(reference, "+", "-")
	difference = strings.ReplaceAll(difference, "+", "-")

	if difference == reference {
		return 0
	}

	splitReference := SplitVersion(reference, delimiter)
	splitDifference := SplitVersion(difference, delimiter)

	if len(splitReference) > 0 && len(splitDifference) > 0 &&
		splitReference[0] == "nightly" && splitDifference[0] == "nightly" {
		if !updateNightly {
			return 0
		}
		dateReference := nightlyDate(splitReference)
		dateDifference := nightlyDate(splitDifference)
		switch {
		case dateDifference > dateReference:
			return 1
		case dateDifference < dateReference:
			return -1
		default:
			return 0
		}
	}

	max := len(splitReference)
	if len(splitDifference) > max {
		max = len(splitDifference)
	}
	for i := 0; i < max; i++ {
		// '1.1-alpha' is less than '1.1'.
		if i >= len(splitReference) {
			if qualifier.MatchString(partString(splitDifference[i])) {
				return -1
			}
			return 1
		}
		// '1.1' is greater than '1.1-beta'.
		if i >= len(splitDifference) {
			if qualifier.MatchString(partString(splitReference[i])) {
				return 1
			}
			return -1
		}
		refPart := splitReference[i]
		diffPart := splitDifference[i]
		refText := partString(refPart)
		diffText := partString(diffPart)

		if strings.Contains(refText, ".") || strings.Contains(diffText, ".") {
			if result := CompareWithOptions(refText, diffText, ".", updateNightly); result != 0 {
				return result
			}
			continue
		}
		if strings.Contains(refText, "_") || strings.Contains(diffText, "_") {
			if result := CompareWithOptions(refText, diffText, "_", updateNightly); result != 0 {
				return result
			}
			continue
		}

		if refNum, ok := refPart.(int64); ok {
			if diffNum, ok := diffPart.(int64); ok {
				switch {
				case diffNum > refNum:
					return 1
				case diffNum < refNum:
					return -1
				}
				continue
			}
		}
		lowerDiff := strings.ToLower(diffText)
		lowerRef := strings.ToLower(refText)
		switch {
		case lowerDiff > lowerRef:
			return 1
		case lowerDiff < lowerRef:
			return -1
		}
	}
	return 0
}

// SplitVersion splits version on delimiter, wrapping each letter run with
// the delimiter first and converting digit runs to int64.
// Mirrors lib/versions.ps1 SplitVersion. ${0} keeps the match intact when
// the delimiter is itself a name character (PowerShell "$&" has no such
// greedy parse, but Go would read "$0_" as variable "0_").
func SplitVersion(version, delimiter string) []any {
	wrapped := letterRun.ReplaceAllString(version, delimiter+"${0}"+delimiter)
	parts := strings.Split(wrapped, delimiter)
	out := make([]any, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		if digitRun.MatchString(part) {
			if num, err := strconv.ParseInt(part, 10, 64); err == nil {
				out = append(out, num)
				continue
			}
		}
		out = append(out, part)
	}
	return out
}

// Latest returns the highest version in versions with delimiter, or "" for
// empty input.
func Latest(versions []string, delimiter string, updateNightly bool) string {
	if len(versions) == 0 {
		return ""
	}
	best := versions[0]
	for _, candidate := range versions[1:] {
		if CompareWithOptions(best, candidate, delimiter, updateNightly) > 0 {
			best = candidate
		}
	}
	return best
}

// nightlyDate extracts the yyyyMMdd date at index 1, defaulting to today
// when the part is absent. Non-numeric parts compare as 0.
func nightlyDate(parts []any) int64 {
	if len(parts) < 2 {
		return todayStamp()
	}
	text := partString(parts[1])
	if !digitRun.MatchString(text) {
		return 0
	}
	num, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0
	}
	return num
}

func todayStamp() int64 {
	num, _ := strconv.ParseInt(time.Now().Format("20060102"), 10, 64)
	return num
}

func partString(part any) string {
	if num, ok := part.(int64); ok {
		return strconv.FormatInt(num, 10)
	}
	if text, ok := part.(string); ok {
		return text
	}
	return ""
}
