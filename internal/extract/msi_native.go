// Native MSI reader progress: CFB inspection through mscfb.
//
// Decision: extraction stays on the msiexec /a bridge (a Windows OS
// component, always present; the classic default in
// lib/decompress.ps1:194-197). Full lessmsi parity needs cabinet
// (MS-CAB) plus LZX/MSZIP decompression on top of the CFB container,
// and no pure-Go, go1.22-compatible MS-CAB/LZX module is pinned;
// porting LZX is out of scope for this round. mscfb (pure Go, go 1.18
// directive, no cgo, CGO_ENABLED=0 clean) is adopted as the first
// native-reader increment: container inspection (stream inventory and
// cabinet-stream detection) without changing the extraction path.
// Cabinet payload extraction stays deferred to a later phase.
package extract

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/richardlehane/mscfb"
)

// MsiCFBModule is the compound-file reader backing MSI inspection.
const MsiCFBModule = "github.com/richardlehane/mscfb"

// MsiInfo summarizes an MSI compound file: every stream path plus
// the subset that likely carries cabinet payloads.
type MsiInfo struct {
	// Streams holds slash-joined stream paths, sorted.
	Streams []string
	// Cabinets holds the Streams entries backed by .cab storages.
	Cabinets []string
}

// InspectMsi opens path as a compound file and inventories its
// streams. It reports an error for non-CFB input; cabinet detection
// is advisory only and never affects extraction.
func InspectMsi(path string) (*MsiInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	doc, err := mscfb.New(f)
	if err != nil {
		return nil, fmt.Errorf("cannot inspect %s as MSI compound file: %w", filepath.Base(path), err)
	}
	if len(doc.File) == 0 {
		return nil, fmt.Errorf("cannot inspect %s as MSI compound file: no directory entries", filepath.Base(path))
	}
	info := &MsiInfo{}
	for i, entry := range doc.File {
		if i == 0 || entry == nil {
			continue
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		full := strings.Join(append(append([]string{}, entry.Path...), entry.Name), "/")
		info.Streams = append(info.Streams, full)
		if IsCabinetStream(entry.Name) {
			info.Cabinets = append(info.Cabinets, full)
		}
	}
	sort.Strings(info.Streams)
	sort.Strings(info.Cabinets)
	return info, nil
}

// IsCabinetStream reports .cab-suffixed stream names, the storages
// that carry MSI file payloads. Matching is case-insensitive.
func IsCabinetStream(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".cab")
}
