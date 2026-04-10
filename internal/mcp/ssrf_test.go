// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package mcp

import (
	"net"
	"testing"
)

func TestIsPrivateIP_TableDriven(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		private bool
	}{
		// RFC 1918 - 10.0.0.0/8
		{"10.0.0.1 private", "10.0.0.1", true},
		{"10.0.0.0 network addr", "10.0.0.0", true},
		{"10.255.255.255 broadcast", "10.255.255.255", true},

		// RFC 1918 - 172.16.0.0/12
		{"172.16.0.1 private", "172.16.0.1", true},
		{"172.31.255.255 end of range", "172.31.255.255", true},
		{"172.32.0.1 just outside", "172.32.0.1", false},
		{"172.15.255.255 just below", "172.15.255.255", false},

		// RFC 1918 - 192.168.0.0/16
		{"192.168.1.1 private", "192.168.1.1", true},
		{"192.168.0.0 network addr", "192.168.0.0", true},
		{"192.168.255.255 broadcast", "192.168.255.255", true},
		{"192.167.255.255 just outside", "192.167.255.255", false},

		// Loopback - 127.0.0.0/8
		{"127.0.0.1 loopback", "127.0.0.1", true},
		{"127.0.0.2 loopback alt", "127.0.0.2", true},
		{"127.255.255.255 loopback broadcast", "127.255.255.255", true},

		// Link-local - 169.254.0.0/16
		{"169.254.1.1 link-local", "169.254.1.1", true},
		{"169.254.169.254 metadata endpoint", "169.254.169.254", true},
		{"169.254.0.0 network addr", "169.254.0.0", true},
		{"169.253.255.255 just outside", "169.253.255.255", false},

		// Public addresses
		{"8.8.8.8 Google DNS", "8.8.8.8", false},
		{"1.1.1.1 Cloudflare DNS", "1.1.1.1", false},
		{"104.16.0.1 Cloudflare", "104.16.0.1", false},
		{"203.0.113.1 documentation range", "203.0.113.1", false},
		{"198.51.100.1 documentation range", "198.51.100.1", false},
		{"93.184.216.34 example.com", "93.184.216.34", false},

		// IPv6 loopback
		{"::1 IPv6 loopback", "::1", true},

		// IPv6 unique local (fc00::/7)
		{"fc00::1 unique local", "fc00::1", true},
		{"fd00::1 unique local", "fd00::1", true},
		{"fdff:ffff::1 unique local end", "fdff:ffff::1", true},

		// IPv6 link-local (fe80::/10)
		{"fe80::1 link-local", "fe80::1", true},
		{"fe80::1234:abcd link-local", "fe80::1234:abcd", true},

		// IPv6 global unicast (public)
		{"2001:db8::1 documentation", "2001:db8::1", false},
		{"2606:4700::1 Cloudflare", "2606:4700::1", false},
		{"2607:f8b0::1 Google", "2607:f8b0::1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("could not parse IP %q", tt.ip)
			}
			got := isPrivateIP(ip)
			if got != tt.private {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.private)
			}
		})
	}
}

func TestIsPrivateIP_IPv4MappedIPv6(t *testing.T) {
	// IPv4-mapped IPv6 addresses like ::ffff:10.0.0.1 should be detected
	// as private because they map to RFC 1918 addresses.
	tests := []struct {
		name    string
		ip      string
		private bool
	}{
		{"mapped 10.0.0.1", "::ffff:10.0.0.1", true},
		{"mapped 127.0.0.1", "::ffff:127.0.0.1", true},
		{"mapped 192.168.1.1", "::ffff:192.168.1.1", true},
		{"mapped 8.8.8.8", "::ffff:8.8.8.8", false},
		{"mapped 1.1.1.1", "::ffff:1.1.1.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("could not parse IP %q", tt.ip)
			}
			got := isPrivateIP(ip)
			if got != tt.private {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.private)
			}
		})
	}
}
