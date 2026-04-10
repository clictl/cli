// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"testing"
)

func TestIsPrivateHost_PrivateAddresses(t *testing.T) {
	privateHosts := []string{
		"127.0.0.1",
		"10.0.0.1",
		"192.168.1.1",
		"169.254.169.254",
		"::1",
		"172.16.0.1",
		"172.31.255.255",
		"10.255.255.255",
		"192.168.0.0",
	}
	for _, host := range privateHosts {
		if !isPrivateHost(host) {
			t.Errorf("isPrivateHost(%q) = false, want true", host)
		}
	}
}

func TestIsPrivateHost_PublicAddresses(t *testing.T) {
	publicHosts := []string{
		"8.8.8.8",
		"1.1.1.1",
		"93.184.216.34",
		"203.0.113.1",
	}
	for _, host := range publicHosts {
		if isPrivateHost(host) {
			t.Errorf("isPrivateHost(%q) = true, want false", host)
		}
	}
}

func TestSecureHTTPClient_NotNil(t *testing.T) {
	client := SecureHTTPClient()
	if client == nil {
		t.Fatal("SecureHTTPClient() returned nil")
	}
	if client.Timeout != 15*1e9 {
		t.Errorf("expected 15s timeout, got %v", client.Timeout)
	}
	if client.CheckRedirect == nil {
		t.Error("expected CheckRedirect to be set")
	}
}
