// Package ui emits gscoop diagnostics with the classic severity prefixes
// and TTY-gated decoration.
//
// Prefixes mirror lib/core.ps1:311-314 exactly: "ERROR " (darkred),
// "WARN  " (darkyellow), "INFO  " (darkgray); DEBUG follows the same shape
// and prints only when debugging is on (lib/core.ps1:315-318). Colors apply
// only on terminals and honor NO_COLOR; text is identical with or without
// color (technical plan section 4.2). Progress renders single carriage-
// return lines on TTY only. No emojis.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Severity prefixes (lib/core.ps1:311-314). WARN and INFO carry two spaces.
const (
	PrefixError = "ERROR "
	PrefixWarn  = "WARN  "
	PrefixInfo  = "INFO  "
	PrefixDebug = "DEBUG "
)

// ANSI decoration, applied only when Color is true.
const (
	colorRed    = "\x1b[31m"
	colorYellow = "\x1b[33m"
	colorGray   = "\x1b[90m"
	colorCyan   = "\x1b[36m"
	colorGreen  = "\x1b[32m"
	colorReset  = "\x1b[0m"
	clearLine   = "\x1b[K"
)

// Logger writes diagnostics. Error and Warn go to Err; Info, Debug, and
// Success go to Out. Classic uses Write-Host for every severity (a single
// console stream); the split here preserves the exact prefix text while
// routing severities to conventional streams. Color decorates whole lines
// without changing text.
type Logger struct {
	// Out receives info, debug, and success lines. Defaults to stdout.
	Out io.Writer
	// Err receives error and warning lines. Defaults to stderr.
	Err io.Writer
	// Color enables ANSI decoration. Compute with ColorEnabled.
	Color bool
	// DebugEnabled gates debug lines (DEBUG config or --verbose,
	// lib/core.ps1:316).
	DebugEnabled bool
}

// New returns a Logger on the standard streams with TTY-aware color and
// debug from SCOOP_DEBUG. The --no-color flag forces plain output.
func New(noColorFlag bool, debug bool) *Logger {
	return &Logger{
		Out:          os.Stdout,
		Err:          os.Stderr,
		Color:        ColorEnabled(os.Stdout, noColorFlag),
		DebugEnabled: debug || isEnvTrue("SCOOP_DEBUG"),
	}
}

// isEnvTrue reports whether env unfolds to "true" case-insensitively,
// mirroring -ine 'true' (lib/core.ps1:316).
func isEnvTrue(name string) bool {
	value, ok := os.LookupEnv(name)
	return ok && strings.EqualFold(value, "true")
}

// IsTerminal reports whether f is a character device (console). Pipes and
// redirected files are not terminals.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// ColorEnabled reports whether decoration may apply: never with the
// --no-color flag, never with NO_COLOR present in the environment, and only
// on a terminal.
func ColorEnabled(out *os.File, noColorFlag bool) bool {
	if noColorFlag {
		return false
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	return IsTerminal(out)
}

// decorate wraps line in ANSI color when enabled.
func (l *Logger) decorate(line, color string) string {
	if !l.Color {
		return line
	}
	return color + line + colorReset
}

// out defaults nil Out to stdout; errOut defaults nil Err to stderr.
func (l *Logger) out() io.Writer {
	if l.Out == nil {
		return os.Stdout
	}
	return l.Out
}

// errOut defaults nil Err to stderr.
func (l *Logger) errOut() io.Writer {
	if l.Err == nil {
		return os.Stderr
	}
	return l.Err
}

// Error writes an ERROR line (lib/core.ps1:312).
func (l *Logger) Error(msg string) {
	fmt.Fprintln(l.errOut(), l.decorate(PrefixError+msg, colorRed))
}

// Errorf writes a formatted ERROR line.
func (l *Logger) Errorf(format string, args ...any) {
	l.Error(fmt.Sprintf(format, args...))
}

// Warn writes a WARN line (lib/core.ps1:313).
func (l *Logger) Warn(msg string) {
	fmt.Fprintln(l.errOut(), l.decorate(PrefixWarn+msg, colorYellow))
}

// Warnf writes a formatted WARN line.
func (l *Logger) Warnf(format string, args ...any) {
	l.Warn(fmt.Sprintf(format, args...))
}

// Info writes an INFO line (lib/core.ps1:314).
func (l *Logger) Info(msg string) {
	fmt.Fprintln(l.out(), l.decorate(PrefixInfo+msg, colorGray))
}

// Infof writes a formatted INFO line.
func (l *Logger) Infof(format string, args ...any) {
	l.Info(fmt.Sprintf(format, args...))
}

// Debug writes a DEBUG line only when debugging is on
// (lib/core.ps1:315-318).
func (l *Logger) Debug(msg string) {
	if !l.DebugEnabled {
		return
	}
	fmt.Fprintln(l.out(), l.decorate(PrefixDebug+msg, colorCyan))
}

// Debugf writes a formatted DEBUG line only when debugging is on.
func (l *Logger) Debugf(format string, args ...any) {
	if !l.DebugEnabled {
		return
	}
	l.Debug(fmt.Sprintf(format, args...))
}

// Success writes an undecorated-prefix success line in green when colored
// (lib/core.ps1:344: success has no severity prefix).
func (l *Logger) Success(msg string) {
	fmt.Fprintln(l.out(), l.decorate(msg, colorGreen))
}

// Successf writes a formatted success line.
func (l *Logger) Successf(format string, args ...any) {
	l.Success(fmt.Sprintf(format, args...))
}

// StripANSI removes ANSI escape sequences so tests can assert color and
// plain output carry identical text.
func StripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < '@' || s[j] > '~') {
				j++
			}
			i = j + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// Progress renders single-line carriage-return progress on TTY only.
// Off TTY every method is a no-op and callers print one line per completed
// artifact instead (technical plan section 4.2).
type Progress struct {
	// Out receives progress lines.
	Out io.Writer
	// Enabled gates output; true only on TTY.
	Enabled bool
	started bool
}

// Update rewrites the current progress line when enabled.
func (p *Progress) Update(line string) {
	if !p.Enabled {
		return
	}
	out := p.Out
	if out == nil {
		out = os.Stdout
	}
	fmt.Fprintf(out, "\r%s%s", line, clearLine)
	p.started = true
}

// Done terminates the progress line when enabled and started.
func (p *Progress) Done() {
	if !p.Enabled || !p.started {
		return
	}
	out := p.Out
	if out == nil {
		out = os.Stdout
	}
	fmt.Fprintln(out)
	p.started = false
}

// FormatSize renders byte counts like filesize (lib/core.ps1:346-363):
// one decimal GB/MB/KB above each power of two with "{0:n1}" thousands
// grouping (for example "1,024.0 KB"), else integer bytes.
func FormatSize(length int64) string {
	const (
		gb = int64(1) << 30
		mb = int64(1) << 20
		kb = int64(1) << 10
	)
	switch {
	case length > gb:
		return formatGroupedFloat(float64(length)/float64(gb)) + " GB"
	case length > mb:
		return formatGroupedFloat(float64(length)/float64(mb)) + " MB"
	case length > kb:
		return formatGroupedFloat(float64(length)/float64(kb)) + " KB"
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
