package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Message helpers reproduce the classic severity prefixes from lib/core.ps1:
// "ERROR " and "WARN  " go to out (classic uses Write-Host for both), with
// success lines printed bare. DEBUG lines appear only when debugging is on.
// Colors are omitted entirely, which satisfies NO_COLOR and non-TTY parity
// since the text is identical with or without decoration.

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
	if os.Getenv("SCOOP_DEBUG") == "true" {
		fmt.Fprintf(out, "DEBUG "+format+"\n", args...)
	}
}

// formatTime renders timestamps as the ScoopTypes.Format.ps1xml views do.
func formatTime(t interface {
	Format(string) string
}) string {
	return t.Format("2006-01-02 15:04:05")
}

// filesize mirrors the filesize function in lib/core.ps1.
func filesize(length int64) string {
	const gb = 1 << 30
	const mb = 1 << 20
	const kb = 1 << 10
	switch {
	case length > gb:
		return fmt.Sprintf("%.1f GB", float64(length)/gb)
	case length > mb:
		return fmt.Sprintf("%.1f MB", float64(length)/mb)
	case length > kb:
		return fmt.Sprintf("%.1f KB", float64(length)/kb)
	default:
		return fmt.Sprintf("%d B", length)
	}
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
