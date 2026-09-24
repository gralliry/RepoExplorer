//go:build windows

package main

import (
	"net/url"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// systemProxy reads the WinINET proxy configuration out of the registry, so we
// use the same proxy the system/browser is configured with.
func systemProxy() *url.URL {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return nil
	}
	defer key.Close()

	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return nil
	}

	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil {
		return nil
	}
	raw := strings.TrimSpace(server)
	if raw == "" {
		return nil
	}

	// ProxyServer is either "host:port" or a per-protocol list such as
	// "http=host:port;https=host:port".
	if strings.Contains(server, "=") {
		raw = ""
		for _, part := range strings.Split(server, ";") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				continue
			}
			scheme := strings.ToLower(strings.TrimSpace(kv[0]))
			if scheme != "https" && scheme != "http" {
				continue
			}
			// Prefer an https entry, but fall back to http.
			if raw == "" || scheme == "https" {
				raw = strings.TrimSpace(kv[1])
			}
		}
	}

	if raw == "" {
		return nil
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return nil
	}
	return parsed
}
