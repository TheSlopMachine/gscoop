// Package cli implements the Scoop command-line surface for gscoop.
//
// The parser reproduces lib/getopt.ps1 semantics: short and long options,
// bundled short flags, first-wins terminators ("--" and "--%"), and the exact
// classic error strings. Comparison is case-insensitive, matching PowerShell
// -match behavior. The long "--opt=value" form is not supported by classic
// getopt and is rejected here as well. No external dependencies; no emojis.
package cli

import "strings"

// FlagValue is the value recorded for an option that takes no argument.
// It mirrors the PowerShell $true assigned by getopt for flag options.
const FlagValue = "true"

// Result is the outcome of GetOpt.
type Result struct {
	// Opts maps each supplied option to its value. Keys keep the spelling
	// used on the command line. Flag options store FlagValue.
	Opts map[string]string
	// Rest holds positional arguments in input order, plus every argument
	// after the first "--" or "--%" terminator.
	Rest []string
	// Err holds the exact classic error string, or "" on success.
	Err string
}

// Has reports whether name was supplied, case-insensitively.
func (r Result) Has(name string) bool {
	for k := range r.Opts {
		if strings.EqualFold(k, name) {
			return true
		}
	}
	return false
}

// Get returns the value recorded for name, or "" when absent.
// Use Has to distinguish an absent option from an empty value.
func (r Result) Get(name string) string {
	for k, v := range r.Opts {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

// GetOpt parses argv against shortopts and longopts.
//
// shortopts is a string of single-letter options; a letter followed by ':'
// takes the next argument as its value. longopts lists long names; a name
// ending with '=' takes the next argument as its value. The first "--" or
// "--%" token ends option parsing; it is dropped and the remaining arguments
// pass through verbatim. A lone "-" is positional. The scan continues across
// positional arguments and stops at the first error.
func GetOpt(argv []string, shortopts string, longopts []string) Result {
	res := Result{Opts: make(map[string]string)}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if arg == "--" || arg == "--%" {
			if i+1 < len(argv) {
				res.Rest = append(res.Rest, argv[i+1:]...)
			}
			break
		}
		if strings.HasPrefix(arg, "--") {
			name := arg[2:]
			entry, ok := matchLong(name, longopts)
			if !ok {
				res.Err = "Option --" + name + " not recognized."
				return res
			}
			if strings.HasSuffix(entry, "=") {
				if i == len(argv)-1 {
					res.Err = "Option --" + name + " requires an argument."
					return res
				}
				i++
				res.Opts[name] = argv[i]
			} else {
				res.Opts[name] = FlagValue
			}
			continue
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			chars := []rune(arg)
			for j := 1; j < len(chars); j++ {
				letter := chars[j]
				found, takesArg := matchShort(letter, shortopts)
				if !found {
					res.Err = "Option -" + string(letter) + " not recognized."
					return res
				}
				if takesArg {
					if j != len(chars)-1 || i == len(argv)-1 {
						res.Err = "Option -" + string(letter) + " requires an argument."
						return res
					}
					i++
					res.Opts[string(letter)] = argv[i]
				} else {
					res.Opts[string(letter)] = FlagValue
				}
			}
			continue
		}
		res.Rest = append(res.Rest, arg)
	}
	return res
}

// matchShort finds the first shortopts entry folding-equal to letter.
// A trailing ':' on the matched entry means the option takes an argument.
func matchShort(letter rune, shortopts string) (found bool, takesArg bool) {
	if letter > 127 {
		return false, false
	}
	for i := 0; i < len(shortopts); i++ {
		if foldByteEq(shortopts[i], byte(letter)) {
			return true, i+1 < len(shortopts) && shortopts[i+1] == ':'
		}
	}
	return false, false
}

// matchLong finds the first longopts entry folding-equal to name, ignoring a
// trailing '=' marker. Classic getopt interpolates raw input into a regex;
// this port uses exact comparison, which accepts the same legitimate input.
func matchLong(name string, longopts []string) (string, bool) {
	for _, entry := range longopts {
		if strings.EqualFold(strings.TrimSuffix(entry, "="), name) {
			return entry, true
		}
	}
	return "", false
}

// foldByteEq compares ASCII bytes case-insensitively.
func foldByteEq(a, b byte) bool {
	if 'A' <= a && a <= 'Z' {
		a += 'a' - 'A'
	}
	if 'A' <= b && b <= 'Z' {
		b += 'a' - 'A'
	}
	return a == b
}
