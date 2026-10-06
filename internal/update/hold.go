package update

import (
	"fmt"
	"io"
	"strings"
	"time"

	"gscoop/internal/config"
)

// HoldState reports whether the scoop core self-update is held.
type HoldState int

const (
	// HoldOff means self-update proceeds.
	HoldOff HoldState = iota
	// HoldOn means HOLD_UPDATE_UNTIL is a future date.
	HoldOn
	// HoldExpired means the date passed; the caller clears the key.
	HoldExpired
	// HoldInvalid means the value does not parse; the caller clears it.
	HoldInvalid
)

// ParseHoldUntil mirrors Test-ScoopCoreOnHold (lib/core.ps1:1263-1284):
// empty means not held, a future date holds, a past date re-enables,
// an unparsable value is invalid. Comparison is in local time.
func ParseHoldUntil(raw string, now time.Time) (HoldState, time.Time) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return HoldOff, time.Time{}
	}
	parsed, err := parseDateTime(trimmed)
	if err != nil {
		return HoldInvalid, time.Time{}
	}
	if parsed.After(now) {
		return HoldOn, parsed
	}
	return HoldExpired, parsed
}

// CheckCoreHold reads HOLD_UPDATE_UNTIL from store, prints the classic
// hold warnings, clears expired or invalid values, and reports whether
// self-update must be skipped. The store is saved only when a clear
// happens. Mirrors lib/core.ps1:1263-1284.
func CheckCoreHold(store *config.Store, out io.Writer, now time.Time) bool {
	raw, _ := store.GetString("hold_update_until")
	if raw == "" {
		if _, ok := store.Get("hold_update_until"); !ok {
			return false
		}
	}
	state, until := ParseHoldUntil(raw, now)
	switch state {
	case HoldOn:
		fmt.Fprintf(out, "WARN  Skipping self-update of Scoop Core until %s...\n", until.Local().Format("2006-01-02 15:04:05"))
		fmt.Fprintf(out, "WARN  If you want to update Scoop Core immediately, use 'scoop unhold scoop; scoop update'.\n")
		return true
	case HoldExpired:
		fmt.Fprintf(out, "WARN  Self-update of Scoop Core is enabled again!\n")
		store.Remove("hold_update_until")
		_ = store.Save()
		return false
	case HoldInvalid:
		fmt.Fprintf(out, "ERROR 'hold_update_until' has been set in the wrong format and was removed.\n")
		fmt.Fprintf(out, "ERROR If you want to disable self-update of Scoop Core for a moment,\n")
		fmt.Fprintf(out, "ERROR use 'scoop hold scoop' or 'scoop config hold_update_until <YYYY-MM-DD>/<YYYY/MM/DD>'.\n")
		store.Remove("hold_update_until")
		_ = store.Save()
		return false
	default:
		return false
	}
}

// parseDateTime accepts the round-trip format plus the YYYY-MM-DD and
// YYYY/MM/DD forms the hold help references, mirroring
// [DateTime]::Parse tolerance for the shapes Scoop writes.
func parseDateTime(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
		"2006/01/02",
		"01/02/2006",
	}
	var err error
	var parsed time.Time
	for _, layout := range layouts {
		if parsed, err = time.Parse(layout, s); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparsable date %q", s)
}

// ScoopOutdated mirrors is_scoop_outdated (lib/core.ps1:1251-1261):
// age of LAST_UPDATE at or beyond 3 hours counts as stale. A missing
// or unparsable value stamps now and counts as stale.
func ScoopOutdated(store *config.Store, now time.Time) bool {
	raw, _ := store.GetString("last_update")
	if raw == "" {
		_ = store.Set("last_update", now.Format(time.RFC3339Nano))
		_ = store.Save()
		return true
	}
	parsed, err := parseDateTime(raw)
	if err != nil {
		_ = store.Set("last_update", now.Format(time.RFC3339Nano))
		_ = store.Save()
		return true
	}
	return now.Sub(parsed).Hours() >= 3
}

// StampUpdate writes LAST_UPDATE in round-trip format
// (libexec/scoop-update.ps1:408-423).
func StampUpdate(store *config.Store, now time.Time) {
	_ = store.Set("last_update", now.Format(time.RFC3339Nano))
	_ = store.Save()
}
