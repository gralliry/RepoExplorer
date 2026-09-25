//go:build !windows

package app

import "net/url"

// On non-Windows platforms we fall back to Go's default behaviour, which reads
// HTTP_PROXY / HTTPS_PROXY from the environment.
func systemProxy() *url.URL { return nil }
