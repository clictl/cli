// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package executor

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
)

// TLSConfig holds custom TLS settings for HTTP transports.
// This is a local type since TLS configuration was removed from the spec model in 1.0.
type TLSConfig struct {
	CACert       string `yaml:"ca_cert,omitempty" json:"ca_cert,omitempty"`
	CACertEnv    string `yaml:"ca_cert_env,omitempty" json:"ca_cert_env,omitempty"`
	ClientCert   string `yaml:"client_cert,omitempty" json:"client_cert,omitempty"`
	ClientCertEnv string `yaml:"client_cert_env,omitempty" json:"client_cert_env,omitempty"`
	ClientKey    string `yaml:"client_key,omitempty" json:"client_key,omitempty"`
	ClientKeyEnv string `yaml:"client_key_env,omitempty" json:"client_key_env,omitempty"`
	ServerName   string `yaml:"server_name,omitempty" json:"server_name,omitempty"`
	MinVersion   string `yaml:"min_version,omitempty" json:"min_version,omitempty"`
	SkipVerify   bool   `yaml:"skip_verify,omitempty" json:"skip_verify,omitempty"`
}

// BuildTLSTransport creates an *http.Transport with custom TLS settings
// based on the provided TLSConfig. Supports CA certificates, client
// certificates for mTLS, server name override, and minimum TLS version.
func BuildTLSTransport(cfg *TLSConfig) (*http.Transport, error) {
	if cfg == nil {
		return nil, fmt.Errorf("TLS config is nil")
	}

	tlsCfg := &tls.Config{}

	// Skip verification (insecure)
	if cfg.SkipVerify {
		fmt.Fprintln(os.Stderr, "WARNING: TLS certificate verification is disabled. This is insecure.")
		tlsCfg.InsecureSkipVerify = true
	}

	// Load CA certificate
	caCertPEM, err := resolveFileOrEnv(cfg.CACert, cfg.CACertEnv)
	if err != nil {
		return nil, fmt.Errorf("loading CA certificate: %w", err)
	}
	if caCertPEM != nil {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCertPEM) {
			return nil, fmt.Errorf("failed to parse CA certificate PEM")
		}
		tlsCfg.RootCAs = pool
	}

	// Load client certificate for mTLS
	clientCertPEM, err := resolveFileOrEnv(cfg.ClientCert, cfg.ClientCertEnv)
	if err != nil {
		return nil, fmt.Errorf("loading client certificate: %w", err)
	}
	clientKeyPEM, err := resolveFileOrEnv(cfg.ClientKey, cfg.ClientKeyEnv)
	if err != nil {
		return nil, fmt.Errorf("loading client key: %w", err)
	}
	if clientCertPEM != nil && clientKeyPEM != nil {
		cert, err := tls.X509KeyPair(clientCertPEM, clientKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("parsing client certificate/key pair: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	// Server name override
	if cfg.ServerName != "" {
		tlsCfg.ServerName = cfg.ServerName
	}

	// Minimum TLS version
	if cfg.MinVersion != "" {
		minVer, err := parseTLSVersion(cfg.MinVersion)
		if err != nil {
			return nil, err
		}
		tlsCfg.MinVersion = minVer
	}

	return &http.Transport{
		TLSClientConfig:    tlsCfg,
		DisableCompression: true, // We handle decompression for caching
	}, nil
}

// resolveFileOrEnv reads content from a file path or an environment variable.
// Returns nil if neither is set. File path takes precedence over env var.
func resolveFileOrEnv(filePath, envVar string) ([]byte, error) {
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("reading file %q: %w", filePath, err)
		}
		return data, nil
	}
	if envVar != "" {
		val := os.Getenv(envVar)
		if val == "" {
			return nil, fmt.Errorf("environment variable %q is not set", envVar)
		}
		return []byte(val), nil
	}
	return nil, nil
}

// parseTLSVersion converts a version string to a tls.Version constant.
func parseTLSVersion(version string) (uint16, error) {
	switch version {
	case "1.2":
		return tls.VersionTLS12, nil
	case "1.3":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("unsupported TLS min_version %q (use 1.2 or 1.3)", version)
	}
}
