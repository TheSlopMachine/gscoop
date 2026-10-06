// Tests for hash parsing and verification against
// testdata/download/hash-vectors.json. Classic basis:
// lib/download.ps1:715-773. No emojis.
package download

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type hashVectors struct {
	Vectors []struct {
		Algorithm string `json:"algorithm"`
		Multihash string `json:"multihash"`
		Input     string `json:"input"`
		Expected  string `json:"expected"`
	} `json:"vectors"`
}

func loadHashVectors(t *testing.T) hashVectors {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "testdata", "download", "hash-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vectors hashVectors
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	return vectors
}

func TestParseHash(t *testing.T) {
	cases := []struct {
		in        string
		algorithm string
		expected  string
		wantErr   bool
	}{
		{"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", "sha256", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", false},
		{"SHA256:BA7816BF8F01CFEA414140DE5DAE2223B00361A396177A9CB410FF61F20015AD", "sha256", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", false},
		{"md5:900150983cd24fb0d6963f7d28e6f72", "md5", "900150983cd24fb0d6963f7d28e6f72", false},
		{"sha1:a9993e364706816aba3e25717850c26c9cd0d89d", "sha1", "a9993e364706816aba3e25717850c26c9cd0d89d", false},
		{"sha512:abc", "sha512", "abc", false},
		{"crc32:deadbeef", "", "", true},
	}
	for _, c := range cases {
		algorithm, expected, err := ParseHash(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseHash(%q) succeeded, want error", c.in)
			}
			continue
		}
		if err != nil || algorithm != c.algorithm || expected != c.expected {
			t.Errorf("ParseHash(%q) = %q, %q, %v", c.in, algorithm, expected, err)
		}
	}
}

func TestSumFileVectors(t *testing.T) {
	dir := t.TempDir()
	for _, v := range loadHashVectors(t).Vectors {
		if v.Expected == "" {
			continue
		}
		path := filepath.Join(dir, v.Algorithm+".bin")
		if err := os.WriteFile(path, []byte(v.Input), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := SumFile(path, v.Algorithm)
		if err != nil {
			t.Fatal(err)
		}
		if got != v.Expected {
			t.Errorf("%s(%q) = %s, want %s", v.Algorithm, v.Input, got, v.Expected)
		}
	}
}

func TestCheckFileHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, _ := CheckFileHash(path, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", "app"); !ok {
		t.Error("matching sha256 must verify")
	}
	if ok, _ := CheckFileHash(path, "sha1:a9993e364706816aba3e25717850c26c9cd0d89d", "app"); !ok {
		t.Error("matching sha1 multihash must verify")
	}
	ok, msg := CheckFileHash(path, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ae", "app")
	if ok {
		t.Error("mismatched hash must fail")
	}
	for _, want := range []string{"Hash check failed!", "App:", "Expected:", "Actual:", "First bytes:"} {
		if !strings.Contains(msg, want) {
			t.Errorf("failure message misses %q: %q", want, msg)
		}
	}
	if ok, msg := CheckFileHash(path, "", "app"); !ok || !strings.Contains(msg, "No hash in manifest") {
		t.Errorf("empty hash must warn, got %v %q", ok, msg)
	}
	if ok, _ := CheckFileHash(path, "crc32:deadbeef", "app"); ok {
		t.Error("unsupported algorithm must fail")
	}
}

func TestHashForURL(t *testing.T) {
	urls := []string{"https://h/a.zip", "https://h/b.zip"}
	hashes := []string{"aaa", "bbb"}
	got, err := HashForURL(hashes, urls, "https://h/b.zip")
	if err != nil || got != "bbb" {
		t.Errorf("HashForURL = %q, %v", got, err)
	}
	if got, err := HashForURL(nil, urls, "https://h/a.zip"); err != nil || got != "" {
		t.Errorf("empty hashes = %q, %v", got, err)
	}
	if _, err := HashForURL(hashes, urls, "https://h/c.zip"); err == nil {
		t.Error("unknown URL must error")
	}
}

func TestParsePrivateHosts(t *testing.T) {
	hosts, err := ParsePrivateHosts([]byte(`[{"match": "example\\.com", "headers": "Authorization=Bearer x\nX-Custom=y"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0].Headers["Authorization"] != "Bearer x" || hosts[0].Headers["X-Custom"] != "y" {
		t.Errorf("hosts = %+v", hosts)
	}
	hosts, err = ParsePrivateHosts([]byte(`[{"match": "h", "headers": {"A": "b"}}]`))
	if err != nil || hosts[0].Headers["A"] != "b" {
		t.Errorf("object headers = %+v, %v", hosts, err)
	}
	if _, err := ParsePrivateHosts([]byte(`{"nope": true}`)); err == nil {
		t.Error("non-array must error")
	}
}
