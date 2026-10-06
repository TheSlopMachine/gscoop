// Tests for zip, tar, and single-stream extraction with runtime
// synthesized fixtures in temp dirs. The 7z and bzip2 paths need
// 7z.exe for fixture generation and skip without it. No emojis.
package extract

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

var payload = map[string]string{
	"hello.txt":       "gscoop extract fixture\n",
	"nested/data.txt": "deterministic fixture content 0123456789\n",
}

func writeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for name, content := range entries {
		fw, err := w.Create(name)
		if err != nil {
			f.Close()
			t.Fatal(err)
		}
		if _, err := io.WriteString(fw, content); err != nil {
			f.Close()
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func verifyPayload(t *testing.T, dir string) {
	t.Helper()
	for name, content := range payload {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
		if string(got) != content {
			t.Fatalf("content mismatch %s", name)
		}
	}
}

func writeTar(t *testing.T, w io.Writer) {
	t.Helper()
	tw := tar.NewWriter(w)
	for name, content := range payload {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
}

func findSevenZip() string {
	candidates := []string{filepath.Join(os.Getenv("USERPROFILE"), "scoop", "apps", "7zip", "current", "7z.exe")}
	candidates = append(candidates, "7z")
	for _, c := range candidates {
		if filepath.IsAbs(c) {
			if _, err := os.Stat(c); err == nil {
				return c
			}
			continue
		}
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("7z.exe"); err == nil {
		return p
	}
	return ""
}

func TestExtractZip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.zip")
	writeZip(t, archive, payload)
	dest := filepath.Join(dir, "out")
	done, err := Extract(archive, dest, Options{Removal: true})
	if err != nil || !done {
		t.Fatalf("Extract = %v, %v", done, err)
	}
	verifyPayload(t, dest)
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Error("removal must delete the archive")
	}
}

func TestExtractZipSlip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "evil.zip")
	writeZip(t, archive, map[string]string{"../evil.txt": "x"})
	if _, err := Extract(archive, filepath.Join(dir, "out"), Options{}); err == nil {
		t.Error("zip-slip entry must fail")
	}
	archiveAbs := filepath.Join(dir, "abs.zip")
	writeZip(t, archiveAbs, map[string]string{"C:/evil.txt": "x"})
	if _, err := Extract(archiveAbs, filepath.Join(dir, "out2"), Options{}); err == nil {
		t.Error("drive-letter entry must fail")
	}
}

func TestExtractZipExtractDir(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.zip")
	writeZip(t, archive, map[string]string{"pkg/bin/app.exe": "x", "pkg/readme.txt": "y"})
	dest := filepath.Join(dir, "out")
	done, err := Extract(archive, dest, Options{ExtractDir: "pkg"})
	if err != nil || !done {
		t.Fatalf("Extract = %v, %v", done, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "app.exe")); err != nil {
		t.Errorf("scoped file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "pkg")); !os.IsNotExist(err) {
		t.Error("scope directory must move up")
	}
}

func TestExtractTarGz(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.gz")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	writeTar(t, gz)
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out")
	done, err := Extract(archive, dest, Options{})
	if err != nil || !done {
		t.Fatalf("Extract = %v, %v", done, err)
	}
	verifyPayload(t, dest)
}

func TestExtractTgzAndTar(t *testing.T) {
	for _, name := range []string{"a.tgz", "a.tar"} {
		dir := t.TempDir()
		archive := filepath.Join(dir, name)
		f, err := os.Create(archive)
		if err != nil {
			t.Fatal(err)
		}
		if name == "a.tgz" {
			gz := gzip.NewWriter(f)
			writeTar(t, gz)
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
		} else {
			writeTar(t, f)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(dir, "out")
		done, err := Extract(archive, dest, Options{})
		if err != nil || !done {
			t.Fatalf("%s: Extract = %v, %v", name, done, err)
		}
		verifyPayload(t, dest)
	}
}

func TestExtractTarXzAndZst(t *testing.T) {
	dir := t.TempDir()
	xzPath := filepath.Join(dir, "a.tar.xz")
	xf, err := os.Create(xzPath)
	if err != nil {
		t.Fatal(err)
	}
	xw, err := xz.NewWriter(xf)
	if err != nil {
		t.Fatal(err)
	}
	writeTar(t, xw)
	if err := xw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := xf.Close(); err != nil {
		t.Fatal(err)
	}
	xzDest := filepath.Join(dir, "out-xz")
	if done, err := Extract(xzPath, xzDest, Options{}); err != nil || !done {
		t.Fatalf("tar.xz: Extract = %v, %v", done, err)
	}
	verifyPayload(t, xzDest)

	zstPath := filepath.Join(dir, "a.tar.zst")
	zf, err := os.Create(zstPath)
	if err != nil {
		t.Fatal(err)
	}
	zw, err := zstd.NewWriter(zf)
	if err != nil {
		t.Fatal(err)
	}
	writeTar(t, zw)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zf.Close(); err != nil {
		t.Fatal(err)
	}
	zstDest := filepath.Join(dir, "out-zst")
	if done, err := Extract(zstPath, zstDest, Options{}); err != nil || !done {
		t.Fatalf("tar.zst: Extract = %v, %v", done, err)
	}
	verifyPayload(t, zstDest)
}

func TestExtractSingleStreams(t *testing.T) {
	raw := []byte("single stream payload\n")
	var gzBuf, xzBuf, zstBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	if _, err := gz.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	xw, err := xz.NewWriter(&xzBuf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := xw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := xw.Close(); err != nil {
		t.Fatal(err)
	}
	zw, err := zstd.NewWriter(&zstBuf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	archives := map[string][]byte{"f.gz": gzBuf.Bytes(), "f.xz": xzBuf.Bytes(), "f.zst": zstBuf.Bytes()}
	for name, data := range archives {
		dir := t.TempDir()
		archive := filepath.Join(dir, name)
		if err := os.WriteFile(archive, data, 0o644); err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(dir, "out")
		done, err := Extract(archive, dest, Options{})
		if err != nil || !done {
			t.Fatalf("%s: Extract = %v, %v", name, done, err)
		}
		got, err := os.ReadFile(filepath.Join(dest, "f"))
		if err != nil || !bytes.Equal(got, raw) {
			t.Errorf("%s content = %q, %v", name, got, err)
		}
	}
}

func TestExtractTarSlip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "evil.tar.gz")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "../evil.txt", Mode: 0o644, Size: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Extract(archive, filepath.Join(dir, "out"), Options{}); err == nil {
		t.Error("tar-slip entry must fail")
	}
}

func TestExtractSevenZip(t *testing.T) {
	sz := findSevenZip()
	if sz == "" {
		t.Skip("no 7z.exe for fixture generation")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range payload {
		p := filepath.Join(src, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	archive := filepath.Join(dir, "a.7z")
	cmd := exec.Command(sz, "a", "-t7z", "-bd", "-y", archive, "hello.txt", "nested")
	cmd.Dir = src
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("7z fixture: %v\n%s", err, output)
	}
	dest := filepath.Join(dir, "out")
	done, err := Extract(archive, dest, Options{})
	if err != nil || !done {
		t.Fatalf("Extract = %v, %v", done, err)
	}
	verifyPayload(t, dest)
}

func TestExtractMultiVolumeError(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.7z.001")
	if err := os.WriteFile(archive, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Extract(archive, filepath.Join(dir, "out"), Options{})
	need, ok := err.(*NeedExternalError)
	if !ok || need == nil {
		t.Fatalf("expected NeedExternalError, got %T %v", err, err)
	}
}
