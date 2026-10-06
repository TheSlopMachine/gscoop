package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

func holdUntilTomorrow() string {
	return time.Now().Add(24 * time.Hour).Format(time.RFC3339Nano)
}

func writeHoldFile(path string, hold bool) error {
	var doc map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		doc = map[string]any{}
	} else {
		if err := json.Unmarshal(data, &doc); err != nil {
			return err
		}
		if doc == nil {
			doc = map[string]any{}
		}
	}
	if hold {
		doc["hold"] = true
	} else {
		delete(doc, "hold")
	}
	// Ordered writer: architecture, bucket, url, hold.
	order := []string{"architecture", "bucket", "url", "hold"}
	var out string
	out += "{\n"
	first := true
	emit := func(k string) {
		v, ok := doc[k]
		if !ok {
			return
		}
		raw, _ := json.Marshal(v)
		if !first {
			out += ",\n"
		}
		out += fmt.Sprintf("    %q: %s", k, raw)
		first = false
	}
	for _, k := range order {
		emit(k)
	}
	for k := range doc {
		known := false
		for _, o := range order {
			if o == k {
				known = true
			}
		}
		if !known {
			emit(k)
		}
	}
	out += "\n}\r\n"
	tmp := path + ".new"
	if err := os.WriteFile(tmp, []byte(out), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
