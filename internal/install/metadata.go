package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// WriteInstallMetadata commits scoop-manifest.json plus
// scoop-install.json through temp plus rename, mirroring
// save_installed_manifest plus save_install_info
// (lib/manifest.ps1:123-147). The commit is the atomic point per
// package, matching failed() semantics.
func WriteInstallMetadata(dir string, op Op, manifestRaw []byte) error {
	if len(manifestRaw) > 0 {
		if err := atomicWrite(filepath.Join(dir, "scoop-manifest.json"), manifestRaw); err != nil {
			return err
		}
	}
	info := orderedInstallInfo(op)
	var b strings.Builder
	b.WriteString("{\n")
	for i, kv := range info {
		raw, err := json.Marshal(kv.value)
		if err != nil {
			return err
		}
		keyRaw, _ := json.Marshal(kv.key)
		fmt.Fprintf(&b, "    %s: %s", keyRaw, raw)
		if i+1 < len(info) {
			b.WriteString(",")
		}
		b.WriteString("\r\n")
	}
	b.WriteString("}\r\n")
	return atomicWrite(filepath.Join(dir, "scoop-install.json"), []byte(b.String()))
}

type kv struct {
	key   string
	value any
}

func orderedInstallInfo(op Op) []kv {
	out := []kv{{key: "architecture", value: op.Architecture}}
	if op.Bucket != "" {
		out = append(out, kv{key: "bucket", value: op.Bucket})
	}
	if op.URL != "" {
		out = append(out, kv{key: "url", value: op.URL})
	}
	// hold:true appends only when held; absence means unheld.
	sort.SliceStable(out, func(i, j int) bool {
		order := map[string]int{"architecture": 0, "bucket": 1, "url": 2, "hold": 3}
		return order[out[i].key] < order[out[j].key]
	})
	return out
}

// WriteHold sets hold:true through the ordered writer without
// disturbing other keys, mirroring scoop-hold.ps1:49-68.
func WriteHold(dir string, hold bool) error {
	path := filepath.Join(dir, "scoop-install.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	if hold {
		doc["hold"] = true
	} else {
		delete(doc, "hold")
	}
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, k)
	}
	order := map[string]int{"architecture": 0, "bucket": 1, "url": 2, "hold": 3}
	sort.Slice(keys, func(i, j int) bool {
		oi, oki := order[keys[i]]
		oj, okj := order[keys[j]]
		if oki && okj {
			return oi < oj
		}
		if oki {
			return true
		}
		if okj {
			return false
		}
		return keys[i] < keys[j]
	})
	var b strings.Builder
	b.WriteString("{\n")
	for i, k := range keys {
		raw, _ := json.Marshal(doc[k])
		keyRaw, _ := json.Marshal(k)
		fmt.Fprintf(&b, "    %s: %s", keyRaw, raw)
		if i+1 < len(keys) {
			b.WriteString(",")
		}
		b.WriteString("\r\n")
	}
	b.WriteString("}\r\n")
	return atomicWrite(path, []byte(b.String()))
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
