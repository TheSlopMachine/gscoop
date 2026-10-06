package install

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/TheSlopMachine/gscoop/internal/hook"
)

// ShowNotes prints manifest notes with substitution, mirroring
// show_notes (lib/install.ps1:355-362).
func ShowNotes(out io.Writer, notes string, vars hook.Vars) {
	if out == nil || strings.TrimSpace(notes) == "" {
		return
	}
	text := hook.Substitute(notes, vars)
	fmt.Fprintln(out, "Notes")
	fmt.Fprintln(out, "-----")
	fmt.Fprintln(out, text)
	fmt.Fprintln(out, "-----")
	_ = os.Stdout
}
