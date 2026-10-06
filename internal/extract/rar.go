// RAR extraction via nwaples/rardecode (RAR4/RAR5, pure Go).
//
// Classic runs 7z over the first split part and deletes split siblings
// after extraction (lib/decompress.ps1:137-139). Go decodes with
// rardecode.OpenReader, which spans multi-volume sets itself (new
// .partN.rar and old .r00 schemes), so no pre-assembly is needed.
// SplitRarParts and AssembleSplitRar stay for sibling-removal parity
// (RemoveArchive) and split-set discovery, not for decoding.
//
// RAR4 and RAR5 entries decode, including solid archives and SFX
// first volumes (signature scan skips the stub). rardecode reports
// failures opaquely (its sentinels are unexported), so decode errors
// wrap with guidance: gscoop carries no password plumbing, and
// password-protected archives need 7z.exe with the password
// (USE_EXTERNAL_7ZIP=true). No NeedExternalError originates here:
// every RAR sub-feature either decodes or fails with that guidance;
// the type remains for the ISO and multi-volume 7z paths.
package extract

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/nwaples/rardecode"
)

// RarModule is the RAR decoding dependency, pinned in go.mod.
const RarModule = "github.com/nwaples/rardecode"

// ExtractRar expands a RAR archive into dest with ExtractDir staging
// identical to the zip path.
func ExtractRar(path, dest, extractDir string) error {
	rc, err := rardecode.OpenReader(path, "")
	if err != nil {
		return rarOpenError(path, err)
	}
	defer rc.Close()
	target := dest
	var staging string
	if extractDir != "" {
		staging = dest + "_tmp"
		target = staging
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	for {
		header, err := rc.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return rarDecodeError(path, err)
		}
		if err := extractRarEntry(rc, header, target); err != nil {
			return err
		}
	}
	if staging != "" {
		return ApplyExtractDir(staging, dest, extractDir)
	}
	return nil
}

// rarOpenError wraps volume-open failures. A missing path returns
// unwrapped; anything else means no RAR signature or unsupported
// header features, with the password hint attached.
func rarOpenError(path string, err error) error {
	if os.IsNotExist(err) {
		return err
	}
	return fmt.Errorf("cannot open %s as RAR archive: %w (if the archive is password-protected, extract it with 7z.exe and the password under USE_EXTERNAL_7ZIP=true)", filepath.Base(path), err)
}

// rarDecodeError wraps mid-archive failures. os.IsNotExist marks a
// missing split volume by type, never by message text.
func rarDecodeError(path string, err error) error {
	if os.IsNotExist(err) {
		return fmt.Errorf("failed to extract files from %s: missing split volume %w", filepath.Base(path), err)
	}
	return fmt.Errorf("failed to extract files from %s: %w (if the archive is password-protected, extract it with 7z.exe and the password under USE_EXTERNAL_7ZIP=true)", filepath.Base(path), err)
}

// extractRarEntry writes one RAR member. Directories materialize;
// links never do, matching the zip and tar engines.
func extractRarEntry(r io.Reader, header *rardecode.FileHeader, dest string) error {
	out, err := JoinSecure(dest, header.Name)
	if err != nil {
		return err
	}
	if header.IsDir {
		return os.MkdirAll(out, 0o755)
	}
	if header.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	w, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(w, r)
	if err := w.Close(); err != nil {
		return err
	}
	return copyErr
}

// SplitRarParts lists the part files of the set containing first,
// sorted in volume order. Missing volumes error: a partial set must
// never decode silently.
func SplitRarParts(first string) ([]string, error) {
	base, ok := SplitRarBase(filepath.Base(first))
	if !ok {
		return []string{first}, nil
	}
	pattern := filepath.Join(filepath.Dir(first), base+".part*.rar")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	sort.Strings(matches)
	return matches, nil
}

// AssembleSplitRar concatenates ordered part files into out, mirroring
// the sibling-removal parity a multi-volume cleanup needs.
func AssembleSplitRar(parts []string, out string) error {
	if len(parts) == 0 {
		return io.ErrUnexpectedEOF
	}
	w, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	for _, part := range parts {
		if err := appendPart(w, part); err != nil {
			w.Close()
			return err
		}
	}
	return w.Close()
}

func appendPart(w *os.File, part string) error {
	in, err := os.Open(part)
	if err != nil {
		return err
	}
	defer in.Close()
	_, err = io.Copy(w, in)
	return err
}
