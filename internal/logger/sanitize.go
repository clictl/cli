// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package logger

import (
	"net/url"
	"strings"
)

// sensitiveParams is the list of query parameter names that may contain secrets.
var sensitiveParams = []string{
	"token",
	"key",
	"api_key",
	"access_token",
	"secret",
	"password",
	"auth",
}

// SanitizeURL removes sensitive query parameters and userinfo from a URL.
// Sensitive parameters (token, key, api_key, access_token, secret, password, auth)
// are replaced with [REDACTED]. Userinfo (username:password in the URL) is also redacted.
// Returns "[invalid-url]" if the URL cannot be parsed.
func SanitizeURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "[invalid-url]"
	}

	// Redact sensitive query parameters
	q := parsed.Query()
	redacted := false
	for _, param := range sensitiveParams {
		// Case-insensitive check: iterate all keys
		for qKey := range q {
			if strings.EqualFold(qKey, param) {
				q.Set(qKey, "[REDACTED]")
				redacted = true
			}
		}
	}
	if redacted {
		parsed.RawQuery = q.Encode()
	}

	// Redact userinfo
	if parsed.User != nil {
		parsed.User = url.UserPassword("[REDACTED]", "[REDACTED]")
	}

	return parsed.String()
}
