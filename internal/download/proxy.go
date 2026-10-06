// Proxy handling: PROXY config parsing and HTTP transport selection.
//
// The grammar mirrors setup_proxy (lib/download.ps1:560-587):
// [credentials@]address, where credentials split user:password on the
// first unescaped colon and @ and : escape as \@ and \:. The address
// "none" disables the proxy, "default" keeps the system proxy, and the
// credential "currentuser" selects the ambient Windows credentials.
// Parse failures surface as errors so callers warn and keep the system
// proxy, matching the classic catch-and-warn.
package download

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ProxyMode selects the proxy behavior.
type ProxyMode int

const (
	// ProxySystem keeps the system proxy (empty or "default" setting).
	ProxySystem ProxyMode = iota
	// ProxyDirect disables the proxy ("none" setting).
	ProxyDirect
	// ProxyCustom routes through Address with optional credentials.
	ProxyCustom
)

// ProxyConfig is a parsed PROXY value.
type ProxyConfig struct {
	Mode ProxyMode
	// Address is host[:port] for ProxyCustom.
	Address string
	// Username and Password come from credentials@.
	Username string
	Password string
	// UseDefaultCredentials mirrors currentuser@: ambient Windows
	// credentials. Go has no integrated Windows proxy auth without extra
	// dependencies, so the client keeps the system proxy and surfaces a
	// notice instead (documented gap).
	UseDefaultCredentials bool
}

// splitUnescaped splits s on sep runes not preceded by a backslash.
func splitUnescaped(s string, sep byte) []string {
	var parts []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && (s[i+1] == '@' || s[i+1] == ':') {
			cur.WriteByte(s[i+1])
			i++
			continue
		}
		if s[i] == sep {
			parts = append(parts, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(s[i])
	}
	parts = append(parts, cur.String())
	return parts
}

// ParseProxySetting parses a PROXY config value (setup_proxy,
// lib/download.ps1:560-587).
func ParseProxySetting(setting string) (ProxyConfig, error) {
	if setting == "" {
		return ProxyConfig{Mode: ProxySystem}, nil
	}
	at := splitUnescaped(setting, '@')
	var credentials, address string
	if len(at) < 2 {
		address = at[0]
	} else {
		credentials, address = at[0], at[1]
	}
	switch address {
	case "none":
		return ProxyConfig{Mode: ProxyDirect}, nil
	case "default":
		cfg := ProxyConfig{Mode: ProxySystem}
		if credentials == "currentuser" {
			cfg.UseDefaultCredentials = true
		}
		return cfg, nil
	}
	if address == "" {
		return ProxyConfig{}, fmt.Errorf("proxy %q has no address", setting)
	}
	cfg := ProxyConfig{Mode: ProxyCustom, Address: address}
	switch {
	case credentials == "":
	case credentials == "currentuser":
		cfg.UseDefaultCredentials = true
	default:
		parts := splitUnescaped(credentials, ':')
		cfg.Username = parts[0]
		if len(parts) > 1 {
			cfg.Password = parts[1]
		}
	}
	return cfg, nil
}

// ProxyURL renders the fixed proxy URL for ProxyCustom, embedding user
// info so the transport emits Proxy-Authorization.
func (c ProxyConfig) ProxyURL() (*url.URL, error) {
	if c.Mode != ProxyCustom {
		return nil, nil
	}
	u, err := url.Parse("http://" + c.Address)
	if err != nil {
		return nil, err
	}
	if c.Username != "" {
		u.User = url.UserPassword(c.Username, c.Password)
	}
	return u, nil
}

// NewHTTPClient builds a client honoring the PROXY setting. The notice
// carries currentuser@ and failure warnings for the caller to log,
// mirroring the classic warn-and-continue.
func NewHTTPClient(proxySetting string) (*http.Client, string, error) {
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment}
	cfg, err := ParseProxySetting(proxySetting)
	if err != nil {
		return &http.Client{Transport: transport},
			fmt.Sprintf("Failed to use proxy '%s': %s", proxySetting, err),
			nil
	}
	var notice string
	switch cfg.Mode {
	case ProxyDirect:
		transport.Proxy = nil
	case ProxyCustom:
		proxyURL, err := cfg.ProxyURL()
		if err != nil {
			return &http.Client{Transport: transport},
				fmt.Sprintf("Failed to use proxy '%s': %s", proxySetting, err),
				nil
		}
		transport.Proxy = http.ProxyURL(proxyURL)
		if cfg.UseDefaultCredentials {
			notice = "proxy uses currentuser credentials: integrated Windows proxy authentication is not supported, continuing without explicit credentials"
		}
	case ProxySystem:
		if cfg.UseDefaultCredentials {
			notice = "proxy uses currentuser credentials: integrated Windows proxy authentication is not supported, using system proxy configuration"
		}
	}
	return &http.Client{Transport: transport}, notice, nil
}
