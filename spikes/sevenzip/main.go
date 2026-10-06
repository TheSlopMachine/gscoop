// Command sevenzip-spike verifies archive extraction for the composite
// pure-Go stack defined in the technical plan section 8.3, option B.
//
// Fixtures are generated locally with the standard library (zip, tar.gz).
// The 7z fixture is generated with 7z.exe when present; the 7z fixture is
// then extracted with github.com/bodgit/sevenzip. zip and tar.gz fixtures
// are extracted with archive/zip, archive/tar, and compress/gzip.
//
// Exit code is 0 when every executed check passes, 1 otherwise.
// A missing 7z.exe produces a SKIP for the 7z case, not a failure.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"

	"github.com/bodgit/sevenzip"
)

var failures int

func report(status, name, detail string) {
	if status == "FAIL" {
		failures++
	}
	fmt.Printf("%s %s %s\n", status, name, detail)
}

var payload = map[string]string{
	"hello.txt":        "gscoop sevenzip spike\n",
	"nested/data.txt":  "deterministic fixture content 0123456789\n",
	"nested/empty.txt": "",
}

func writePayload(dir string) error {
	for name, content := range payload {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func verifyDir(dir string) (bool, string) {
	for name, content := range payload {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return false, "missing " + name + ": " + err.Error()
		}
		if string(got) != content {
			return false, "content mismatch " + name
		}
	}
	return true, fmt.Sprintf("files=%d", len(payload))
}

func makeZip(path, srcDir string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := zip.NewWriter(f)
	defer w.Close()
	for name, content := range payload {
		fw, err := w.Create(name)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(fw, content); err != nil {
			return err
		}
	}
	return nil
}

func extractZip(path, dest string) (int, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return 0, err
	}
	defer r.Close()
	count := 0
	for _, f := range r.File {
		out := filepath.Join(dest, f.Name)
		if f.FileInfo().IsDir() {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return count, err
		}
		rc, err := f.Open()
		if err != nil {
			return count, err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return count, err
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func makeTarGz(path, srcDir string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	for name, content := range payload {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}); err != nil {
			return err
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(path, dest string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return 0, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	count := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, err
		}
		out := filepath.Join(dest, hdr.Name)
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return count, err
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return count, err
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func findSevenZip() string {
	candidates := []string{
		filepath.Join(os.Getenv("USERPROFILE"), "scoop", "apps", "7zip", "current", "7z.exe"),
		"7z",
	}
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
	return ""
}

func extractSevenZip(path, dest string) (int, error) {
	r, err := sevenzip.OpenReader(path)
	if err != nil {
		return 0, err
	}
	defer r.Close()
	count := 0
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		out := filepath.Join(dest, f.Name)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return count, err
		}
		rc, err := f.Open()
		if err != nil {
			return count, err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return count, err
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func main() {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == "github.com/bodgit/sevenzip" {
				fmt.Printf("INFO sevenzip-dep version=%s\n", dep.Version)
			}
		}
	}

	root, err := os.MkdirTemp("", "sevenzip-spike-")
	if err != nil {
		fmt.Printf("FAIL setup temp: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(root)

	srcDir := filepath.Join(root, "src")
	if err := os.MkdirAll(filepath.Join(srcDir, "nested"), 0o755); err != nil {
		fmt.Printf("FAIL setup src: %v\n", err)
		os.Exit(1)
	}
	if err := writePayload(srcDir); err != nil {
		fmt.Printf("FAIL setup payload: %v\n", err)
		os.Exit(1)
	}

	zipPath := filepath.Join(root, "fixture.zip")
	if err := makeZip(zipPath, srcDir); err != nil {
		report("FAIL", "zip-create", err.Error())
	} else {
		report("PASS", "zip-create", zipPath)
	}
	zipDest := filepath.Join(root, "out-zip")
	if err := os.MkdirAll(zipDest, 0o755); err != nil {
		report("FAIL", "zip-extract", err.Error())
	} else if n, err := extractZip(zipPath, zipDest); err != nil {
		report("FAIL", "zip-extract", err.Error())
	} else if ok, detail := verifyDir(zipDest); !ok {
		report("FAIL", "zip-verify", detail)
	} else {
		report("PASS", "zip-extract-verify", fmt.Sprintf("entries=%d %s", n, detail))
	}

	tgzPath := filepath.Join(root, "fixture.tar.gz")
	if err := makeTarGz(tgzPath, srcDir); err != nil {
		report("FAIL", "tar.gz-create", err.Error())
	} else {
		report("PASS", "tar.gz-create", tgzPath)
	}
	tgzDest := filepath.Join(root, "out-tgz")
	if err := os.MkdirAll(tgzDest, 0o755); err != nil {
		report("FAIL", "tar.gz-extract", err.Error())
	} else if n, err := extractTarGz(tgzPath, tgzDest); err != nil {
		report("FAIL", "tar.gz-extract", err.Error())
	} else if ok, detail := verifyDir(tgzDest); !ok {
		report("FAIL", "tar.gz-verify", detail)
	} else {
		report("PASS", "tar.gz-extract-verify", fmt.Sprintf("entries=%d %s", n, detail))
	}

	sz := findSevenZip()
	if sz == "" {
		report("SKIP", "7z-bodgit", "no 7z.exe available for fixture generation")
	} else {
		szPath := filepath.Join(root, "fixture.7z")
		cmd := exec.Command(sz, "a", "-t7z", "-bd", "-y", szPath, "hello.txt", "nested")
		cmd.Dir = srcDir
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if out, err := cmd.Output(); err != nil {
			report("FAIL", "7z-create", fmt.Sprintf("%v %s", err, stderr.String()))
		} else {
			_ = out
			report("PASS", "7z-create", szPath+" via "+sz)
			szDest := filepath.Join(root, "out-7z")
			if err := os.MkdirAll(szDest, 0o755); err != nil {
				report("FAIL", "7z-bodgit-extract", err.Error())
			} else if n, err := extractSevenZip(szPath, szDest); err != nil {
				report("FAIL", "7z-bodgit-extract", err.Error())
			} else if ok, detail := verifyDir(szDest); !ok {
				report("FAIL", "7z-bodgit-verify", fmt.Sprintf("entries=%d %s", n, detail))
			} else {
				report("PASS", "7z-bodgit-extract-verify", fmt.Sprintf("entries=%d %s", n, detail))
			}
		}
	}

	if failures > 0 {
		os.Exit(1)
	}
}
