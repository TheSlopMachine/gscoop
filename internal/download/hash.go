// Hash verification: manifest hash parsing and file checking.
//
// Install-time hashes mirror get_hash (lib/download.ps1:761-773): plain
// hex defaults to sha256, otherwise an explicit md5/sha1/sha256/sha512
// multihash prefix selects the algorithm. Matching is case-insensitive.
// Index alignment mirrors hash_for_url (lib/download.ps1:715-726); the
// missing-hash warning and OK/failure messages mirror check_hash
// (lib/download.ps1:728-759).
package download

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"

	"gscoop/internal/config"
)

// HashError reports a manifest hash mismatch for one URL. It fails only
// that package; other downloads continue.
type HashError struct {
	URL    string
	Detail string
}

func (e *HashError) Error() string {
	if e.Detail != "" {
		return e.Detail
	}
	return fmt.Sprintf("hash check failed for URL %s", e.URL)
}

// ParseHash splits a manifest hash into algorithm and expected hex
// (get_hash, lib/download.ps1:761-773). A bare value defaults to sha256.
// Unknown algorithms error like the classic unsupported-type message.
func ParseHash(multihash string) (algorithm, expected string, err error) {
	algorithm, expected = "sha256", multihash
	if i := strings.Index(multihash, ":"); i >= 0 {
		algorithm, expected = multihash[:i], multihash[i+1:]
	}
	switch strings.ToLower(algorithm) {
	case "md5", "sha1", "sha256", "sha512":
		return strings.ToLower(algorithm), strings.ToLower(expected), nil
	default:
		return "", "", fmt.Errorf("Hash type '%s' isn't supported.", algorithm)
	}
}

// newHasher returns a hash.Hash for a parsed algorithm name.
func newHasher(algorithm string) hash.Hash {
	switch algorithm {
	case "md5":
		return md5.New()
	case "sha1":
		return sha1.New()
	case "sha512":
		return sha512.New()
	default:
		return sha256.New()
	}
}

// SumFile hashes a file with the named algorithm.
func SumFile(path, algorithm string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := newHasher(algorithm)
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// MagicBytesPretty renders the first 8 bytes as uppercase hex pairs
// (get_magic_bytes_pretty, lib/download.ps1:670-676).
func MagicBytesPretty(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 8)
	n, _ := f.Read(buf)
	parts := make([]string, 0, n)
	for _, b := range buf[:n] {
		parts = append(parts, fmt.Sprintf("%02X", b))
	}
	return strings.Join(parts, " ")
}

// CheckFileHash verifies path against a manifest hash for app display.
// It returns false with a message on mismatch (check_hash,
// lib/download.ps1:728-759). An empty hash warns with the computed
// SHA256 and reports ok, exactly like classic.
func CheckFileHash(path, manifestHash, app string) (ok bool, message string) {
	if manifestHash == "" {
		sum, err := SumFile(path, "sha256")
		if err != nil {
			return false, fmt.Sprintf("could not hash file: %s", err)
		}
		return true, fmt.Sprintf("No hash in manifest. SHA256 for '%s' is:\n    %s", Base(path), sum)
	}
	algorithm, expected, err := ParseHash(manifestHash)
	if err != nil {
		return false, err.Error()
	}
	actual, err := SumFile(path, algorithm)
	if err != nil {
		return false, err.Error()
	}
	if actual != expected {
		var msg strings.Builder
		msg.WriteString("Hash check failed!\n")
		fmt.Fprintf(&msg, "App:         %s\n", app)
		fmt.Fprintf(&msg, "URL:         %s\n", manifestHash)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			fmt.Fprintf(&msg, "First bytes: %s\n", MagicBytesPretty(path))
		}
		if expected != "" || actual != "" {
			fmt.Fprintf(&msg, "Expected:    %s\n", expected)
			fmt.Fprintf(&msg, "Actual:      %s", actual)
		}
		return false, msg.String()
	}
	return true, ""
}

// HashForURL aligns a manifest hash list with its URL list by index
// (hash_for_url, lib/download.ps1:715-726). No hashes means no check.
// A URL missing from the list is an error, like the classic abort.
func HashForURL(hashes []string, urls []string, rawurl string) (string, error) {
	kept := make([]string, 0, len(hashes))
	for _, h := range hashes {
		if h != "" {
			kept = append(kept, h)
		}
	}
	if len(kept) == 0 {
		return "", nil
	}
	for i, u := range urls {
		if u == rawurl && i < len(kept) {
			return kept[i], nil
		}
	}
	return "", fmt.Errorf("Couldn't find hash in manifest for '%s'.", rawurl)
}

// PrivateHost carries one PRIVATE_HOSTS entry: a URL match regex plus
// extra headers (lib/download.ps1:107-111).
type PrivateHost struct {
	Match   string
	Pattern string
	Headers map[string]string
}

// ParsePrivateHosts decodes the PRIVATE_HOSTS config value: an array of
// {match, headers} where headers is StringData text (key=value lines) or
// an object. Unknown shapes error so the caller can surface them.
func ParsePrivateHosts(data []byte) ([]PrivateHost, error) {
	var items []struct {
		Match   string          `json:"match"`
		Headers json.RawMessage `json:"headers"`
	}
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parsing private_hosts: %w", err)
	}
	hosts := make([]PrivateHost, 0, len(items))
	for _, item := range items {
		if item.Match == "" {
			continue
		}
		headers := map[string]string{}
		if len(item.Headers) != 0 && string(item.Headers) != "null" {
			var text string
			if err := json.Unmarshal(item.Headers, &text); err == nil {
				headers = ParseStringData(text)
			} else {
				var obj map[string]string
				if err := json.Unmarshal(item.Headers, &obj); err != nil {
					return nil, fmt.Errorf("parsing private_hosts headers: %w", err)
				}
				headers = obj
			}
		}
		hosts = append(hosts, PrivateHost{Match: item.Match, Pattern: item.Match, Headers: headers})
	}
	return hosts, nil
}

// PrivateHostsFromStore decodes the private_hosts config value. Absent
// or null values yield no hosts; malformed values error.
func PrivateHostsFromStore(cfg *config.Store) ([]PrivateHost, error) {
	if cfg == nil {
		return nil, nil
	}
	raw, ok := cfg.Get("private_hosts")
	if !ok || string(raw) == "null" {
		return nil, nil
	}
	return ParsePrivateHosts(raw)
}

// ParseStringData decodes PowerShell ConvertFrom-StringData text:
// key=value lines, blank lines skipped.
func ParseStringData(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if i := strings.Index(line, "="); i >= 0 {
			out[strings.TrimSpace(line[:i])] = strings.TrimSpace(line[i+1:])
		}
	}
	return out
}
