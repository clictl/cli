// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package mcp

import (
	"net/http/httptest"
)

// newTestTLSTransport creates a StreamableHTTPTransport connected to the given
// httptest.Server, using the server's TLS client and insecure mode enabled.
// This eliminates the repeated boilerplate found across transport tests.
func newTestTLSTransport(srv *httptest.Server) *StreamableHTTPTransport {
	return &StreamableHTTPTransport{
		endpoint:   srv.URL,
		headers:    make(map[string]string),
		httpClient: srv.Client(),
		insecure:   true,
	}
}
