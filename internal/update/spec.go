package update

import (
	"regexp"
	"strings"
)

// appSpecPattern mirrors parse_app (lib/core.ps1:1233-1239):
// optional bucket prefix, app name or .json path, optional @version.
var appSpecPattern = regexp.MustCompile(`^(?:(?P<bucket>[a-zA-Z0-9-_.]+)/)?(?P<app>.*\.json|[a-zA-Z0-9-_.]+)(?:@(?P<version>.*))?$`)

// AppSpec is a parsed app reference: optional bucket, app name,
// optional version pin.
type AppSpec struct {
	App     string
	Bucket  string
	Version string
	Raw     string
}

// ParseAppSpec splits bucket/app@version references. Unmatched input
// passes through as a bare app name.
func ParseAppSpec(spec string) AppSpec {
	out := AppSpec{Raw: spec}
	m := appSpecPattern.FindStringSubmatch(spec)
	if m == nil {
		out.App = spec
		return out
	}
	for i, name := range appSpecPattern.SubexpNames() {
		if i == 0 || name == "" {
			continue
		}
		switch name {
		case "app":
			out.App = m[i]
		case "bucket":
			out.Bucket = m[i]
		case "version":
			out.Version = m[i]
		}
	}
	if out.App == "" {
		out.App = spec
	}
	return out
}

// ShowApp mirrors show_app (lib/core.ps1:1241-1249).
func ShowApp(app, bucket, version string) string {
	if bucket != "" {
		app = bucket + "/" + app
	}
	if version != "" {
		app = app + "@" + version
	}
	return app
}

// StripBucket drops a bucket/ prefix, mirroring the install lookup.
func StripBucket(spec string) string {
	if i := strings.LastIndexAny(spec, "/\\"); i >= 0 {
		return spec[i+1:]
	}
	return spec
}
