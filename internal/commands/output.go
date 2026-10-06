package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Message helpers reproduce the classic severity prefixes from lib/core.ps1:
// "ERROR " and "WARN  " go to out (classic uses Write-Host for both, a single
// console stream), with success lines printed bare. The single-out shape here
// preserves that text parity; internal/ui splits the same text across
// stderr/stdout for conventional stream routing. DEBUG lines appear only when
// debugging is on. Colors are omitted entirely, which satisfies NO_COLOR and
// non-TTY parity since the text is identical with or without decoration.

func Errorf(out io.Writer, format string, args ...any) {
	fmt.Fprintf(out, "ERROR "+format+"\n", args...)
}

func Warnf(out io.Writer, format string, args ...any) {
	fmt.Fprintf(out, "WARN  "+format+"\n", args...)
}

func Infof(out io.Writer, format string, args ...any) {
	fmt.Fprintf(out, "INFO  "+format+"\n", args...)
}

func Successf(out io.Writer, format string, args ...any) {
	fmt.Fprintf(out, format+"\n", args...)
}

func Debugf(out io.Writer, format string, args ...any) {
	if strings.EqualFold(os.Getenv("SCOOP_DEBUG"), "true") {
		fmt.Fprintf(out, "DEBUG "+format+"\n", args...)
	}
}

// formatTime renders timestamps as the ScoopTypes.Format.ps1xml views do.
func formatTime(t interface {
	Format(string) string
}) string {
	return t.Format("2006-01-02 15:04:05")
}

// filesize mirrors the filesize function in lib/core.ps1:346-363. Decimal
// units use one fractional digit with thousands grouping like "{0:n1}"
// (for example "1,024.0 KB" or "1,234.5 MB"); bytes stay ungrouped.
func filesize(length int64) string {
	const gb = 1 << 30
	const mb = 1 << 20
	const kb = 1 << 10
	switch {
	case length > gb:
		return formatGroupedFloat(float64(length)/gb) + " GB"
	case length > mb:
		return formatGroupedFloat(float64(length)/mb) + " MB"
	case length > kb:
		return formatGroupedFloat(float64(length)/kb) + " KB"
	default:
		return fmt.Sprintf("%d B", length)
	}
}

// formatGroupedFloat renders one fractional digit with comma grouping on the
// integer part, matching PowerShell "{0:n1}".
func formatGroupedFloat(value float64) string {
	s := fmt.Sprintf("%.1f", value)
	neg := ""
	if strings.HasPrefix(s, "-") {
		neg = "-"
		s = s[1:]
	}
	intPart := s
	frac := ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart = s[:i]
		frac = s[i:]
	}
	return neg + groupDigits(intPart) + frac
}

// groupDigits inserts commas every three digits from the right.
func groupDigits(digits string) string {
	if len(digits) <= 3 {
		return digits
	}
	var b strings.Builder
	first := len(digits) % 3
	if first == 0 {
		first = 3
	}
	b.WriteString(digits[:first])
	for i := first; i < len(digits); i += 3 {
		b.WriteByte(',')
		b.WriteString(digits[i : i+3])
	}
	return b.String()
}

func pluralize(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// friendlyPath mirrors friendly_path in lib/core.ps1: the user home prefix
// becomes "~".
func friendlyPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if !strings.HasSuffix(home, string(filepath.Separator)) {
		home += string(filepath.Separator)
	}
	if home == string(filepath.Separator) {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + string(filepath.Separator) + strings.TrimPrefix(path, home)
	}
	return path
}

// renderTable writes header plus rows with columns padded to a uniform width
// separated by two spaces. Column order matches the classic ps1xml views.
func renderTable(out io.Writer, header []string, rows [][]string) {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = len(h)
	}
	for _, r := range rows {
		for i := range header {
			if i < len(r) && len(r[i]) > widths[i] {
				widths[i] = len(r[i])
			}
		}
	}
	writeRow := func(cols []string) {
		var line strings.Builder
		for i := range header {
			cell := ""
			if i < len(cols) {
				cell = cols[i]
			}
			if i == len(header)-1 {
				line.WriteString(cell)
			} else {
				fmt.Fprintf(&line, "%-*s  ", widths[i], cell)
			}
		}
		fmt.Fprint(out, strings.TrimRight(line.String(), " ")+"\n")
	}
	writeRow(header)
	for _, r := range rows {
		writeRow(r)
	}
}
