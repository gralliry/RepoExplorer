package main

import (
	"net/http"
	"time"
)

// newHTTPClient builds a client that honours the Windows system proxy, so the
// app behaves like the browser does. Go's default transport only looks at
// HTTP_PROXY / HTTPS_PROXY environment variables.
func newHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL := systemProxy(); proxyURL != nil {
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}
