// Directory moves: movedir parity plus extraction path safety.
//
// movedir merges one directory tree into another through robocopy /e
// /move (lib/core.ps1:864-890). MoveDir ports the merge semantics with
// pure Go: rename fast path, recursive merge otherwise, source removed
// afterwards. ApplyExtractDir ports the ExtractDir hand-off used by the
// zip, msi, and 7z expanders: move tmp/ExtractDir up, drop tmp, and
// remove the now-empty top directory (lib/decompress.ps1:122-129).
// JoinSecure guards every archive member against zip-slip: absolute
// paths and .. escapes fail closed.
package extract

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MoveDir merges from into to, removing from afterwards. Existing files
// in to are overwritten, matching robocopy /move.
func MoveDir(from, to string) error {
	from = strings.TrimRight(from, `\/`)
	to = strings.TrimRight(to, `\/`)
	if from == "" || to == "" {
		return fmt.Errorf("movedir needs source and destination")
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		src := filepath.Join(from, entry.Name())
		dst := filepath.Join(to, entry.Name())
		if err := moveEntry(src, dst, entry.IsDir()); err != nil {
			return err
		}
	}
	return os.Remove(from)
}

// moveEntry moves one file or merges one directory.
func moveEntry(src, dst string, isDir bool) error {
	if !isDir {
		if err := os.Rename(src, dst); err == nil {
			return nil
		}
		if _, err := os.Lstat(dst); err == nil {
			if err := os.Remove(dst); err != nil {
				return err
			}
			if err := os.Rename(src, dst); err == nil {
				return nil
			}
		}
		if err := copyFileEntry(src, dst); err != nil {
			return err
		}
		return os.Remove(src)
	}
	dstInfo, err := os.Stat(dst)
	if err != nil || !dstInfo.IsDir() {
		if err := os.Rename(src, dst); err == nil {
			return nil
		}
		return MoveDir(src, dst)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := moveEntry(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name()), entry.IsDir()); err != nil {
			return err
		}
	}
	return os.Remove(src)
}

// copyFileEntry copies one regular file, creating parents.
func copyFileEntry(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// ApplyExtractDir moves tmp/extractDir up to dest, removes tmp, and
// drops the emptied top directory, mirroring Expand-ZipArchive and
// Expand-7zipArchive (lib/decompress.ps1:122-129, 293-296).
func ApplyExtractDir(tmp, dest, extractDir string) error {
	if extractDir == "" {
		return MoveDir(tmp, dest)
	}
	if err := MoveDir(filepath.Join(tmp, extractDir), dest); err != nil {
		return err
	}
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	top := extractDir
	if i := strings.IndexAny(top, `/\`); i >= 0 {
		top = top[:i]
	}
	topPath := filepath.Join(dest, top)
	entries, err := os.ReadDir(topPath)
	if err != nil || len(entries) != 0 {
		return nil
	}
	return os.Remove(topPath)
}

// JoinSecure joins an archive member name onto dest, rejecting absolute
// paths and escapes above dest.
func JoinSecure(dest, name string) (string, error) {
	slashed := strings.ReplaceAll(name, `\`, "/")
	if filepath.IsAbs(name) || filepath.IsAbs(slashed) || len(slashed) >= 2 && slashed[1] == ':' {
		return "", fmt.Errorf("archive entry %q is absolute", name)
	}
	for _, segment := range strings.Split(slashed, "/") {
		if segment == ".." {
			return "", fmt.Errorf("archive entry %q escapes the destination", name)
		}
	}
	clean := filepath.Clean("/" + slashed)
	rel := strings.TrimPrefix(clean, "/")
	if rel == "" || rel == "." {
		return "", fmt.Errorf("archive entry %q has no path", name)
	}
	target := filepath.Join(dest, filepath.FromSlash(rel))
	if target != dest && !strings.HasPrefix(target, dest+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry %q escapes the destination", name)
	}
	return target, nil
}
