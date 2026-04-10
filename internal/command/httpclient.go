// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"
	"net"
	"net/http"
	"time"
)

// SecureHTTPClient returns an http.Client with redirect restrictions and timeout.
// It limits redirects to 3 hops and blocks redirects to private/loopback IP ranges
// to prevent SSRF attacks.
func SecureHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			if isPrivateHost(req.URL.Hostname()) {
				return fmt.Errorf("redirect to private network blocked: %s", req.URL.Hostname())
			}
			return nil
		},
	}
}

// isPrivateHost returns true if the given host resolves to a private, loopback,
// or link-local IP address. It checks both literal IPs and performs DNS resolution
// to catch DNS rebinding attacks.
func isPrivateHost(host string) bool {
	// First check if host is a literal IP
	if ip := net.ParseIP(host); ip != nil {
		return isPrivateIP(ip)
	}

	// Resolve hostname to IPs and check each one
	ips, err := net.LookupIP(host)
	if err != nil {
		// If we cannot resolve, allow it (DNS may not be available in all contexts)
		return false
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return true
		}
	}
	return false
}

// isPrivateIP checks whether an IP address belongs to a private, loopback,
// or link-local range.
func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}
