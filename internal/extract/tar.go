// Tar extraction: plain tar, compressed tar wrappers, and lone streams.
//
// Classic ran 7z twice for tar wrappers (lib/decompress.ps1:111-121:
// outer decompress, then inner tar). Go streams decompress-then-tar in
// one pass with identical output. Wrappers use gzip (stdlib),
// bzip2 (stdlib), xz (ulikunitz/xz, already pinned), and zstd
// (klauspost/compress, already pinned).
package extract

import (
	"archive/tar"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// ExtractTar expands .tar and compressed tar wrappers into dest.
func ExtractTar(path, dest, extractDir string) error {
	stream, err := openDecompressed(path)
	if err != nil {
		return err
	}
	defer stream.Close()
	target := dest
	var staging string
	if extractDir != "" {
		staging = dest + "_tmp"
		target = staging
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	if err := readTar(stream, target); err != nil {
		return err
	}
	if staging != "" {
		return ApplyExtractDir(staging, dest, extractDir)
	}
	return nil
}

// ExtractSingle decompresses a lone .gz/.bz2/.xz/.zst stream, naming
// the output like 7z: the base name without its last extension.
func ExtractSingle(path, dest string) error {
	stream, err := openDecompressed(path)
	if err != nil {
		return err
	}
	defer stream.Close()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	out, err := JoinSecure(dest, StripExt(filepath.Base(path)))
	if err != nil {
		return err
	}
	data, err := io.ReadAll(stream)
	if err != nil {
		return err
	}
	return os.WriteFile(out, data, 0o644)
}

// decompressedFile wraps a stream with its closer.
type decompressedFile struct {
	io.Reader
	f       *os.File
	closeFn func() error
}

func (d *decompressedFile) Close() error {
	if d.closeFn != nil {
		_ = d.closeFn()
	}
	return d.f.Close()
}

// openDecompressed opens path through the wrapper named by its suffix.
func openDecompressed(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	lowered := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lowered, ".bz2") || strings.HasSuffix(lowered, ".bzip2") || strings.HasSuffix(lowered, ".bz"):
		return &decompressedFile{Reader: bzip2.NewReader(f), f: f}, nil
	case strings.HasSuffix(lowered, ".xz") || strings.HasSuffix(lowered, ".lzma"):
		r, err := xz.NewReader(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		if c, ok := any(r).(io.Closer); ok {
			return &decompressedFile{Reader: r, f: f, closeFn: c.Close}, nil
		}
		return &decompressedFile{Reader: r, f: f}, nil
	case strings.HasSuffix(lowered, ".zst") || strings.HasSuffix(lowered, ".tzst"):
		r, err := zstd.NewReader(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		return &decompressedFile{Reader: r, f: f, closeFn: func() error { r.Close(); return nil }}, nil
	case isGzipName(lowered):
		r, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		return &decompressedFile{Reader: r, f: f, closeFn: r.Close}, nil
	case strings.HasSuffix(lowered, ".tar") || tarWrapperRe.MatchString(path):
		return &decompressedFile{Reader: f, f: f}, nil
	}
	f.Close()
	return nil, fmt.Errorf("no decompressor for %s", filepath.Base(path))
}

// isGzipName matches gzip names including .tgz.
func isGzipName(lowered string) bool {
	return strings.HasSuffix(lowered, ".gz") || strings.HasSuffix(lowered, ".tgz")
}

// readTar streams tar entries into dest. Non-regular entries (links,
// devices) skip safely; escapes fail closed.
func readTar(r io.Reader, dest string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		out, err := JoinSecure(dest, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(out, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				return err
			}
			data, err := io.ReadAll(tr)
			if err != nil {
				return err
			}
			if err := os.WriteFile(out, data, 0o644); err != nil {
				return err
			}
		default:
			// Links, devices, and other special files never
			// materialize; only regular files and directories do.
			continue
		}
	}
}
