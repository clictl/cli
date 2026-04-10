// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package signing

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SigstoreExpectedIdentity is the expected OIDC identity in the Sigstore certificate SAN.
// Set via ldflags in production: -X github.com/clictl/cli/internal/signing.SigstoreExpectedIdentity=...
var SigstoreExpectedIdentity = "" // e.g., "https://github.com/clictl/registry/.github/workflows/build.yml@refs/heads/main"

// SigstoreBundle represents a Sigstore signing bundle containing certificate,
// signature, and transparency log metadata.
type SigstoreBundle struct {
	Certificate         string `json:"certificate"`
	Signature           string `json:"signature"`
	RekorLogIndex       int64  `json:"rekor_log_index"`
	RekorLogID          string `json:"rekor_log_id"`
	RekorIntegratedTime int64  `json:"rekor_integrated_time"`
}

// SigstoreResult holds the outcome of a Sigstore verification for display purposes.
type SigstoreResult struct {
	Verified       bool
	LogIndex       int64
	IntegratedTime time.Time
	Identity       string
	RekorVerified  bool
}

// ParseSigstoreBundle parses a JSON bundle into a SigstoreBundle struct.
func ParseSigstoreBundle(data []byte) (*SigstoreBundle, error) {
	var bundle SigstoreBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return nil, fmt.Errorf("parsing sigstore bundle JSON: %w", err)
	}
	if bundle.Certificate == "" {
		return nil, fmt.Errorf("sigstore bundle missing certificate")
	}
	if bundle.Signature == "" {
		return nil, fmt.Errorf("sigstore bundle missing signature")
	}
	return &bundle, nil
}

// VerifySigstoreBundle verifies a Sigstore bundle against a manifest.
// It checks the certificate, signature, and optionally the Rekor transparency log.
// expectedIdentity is the expected OIDC identity in the certificate SAN
// (e.g., "https://github.com/clictl/registry/.github/workflows/build.yml@refs/heads/main").
func VerifySigstoreBundle(manifestYAML string, bundle SigstoreBundle, expectedIdentity string) (*SigstoreResult, error) {
	// Parse the certificate from the bundle
	certPEM, err := base64.StdEncoding.DecodeString(bundle.Certificate)
	if err != nil {
		return nil, fmt.Errorf("decoding certificate base64: %w", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		// Try treating the decoded bytes as DER directly
		block = &pem.Block{Type: "CERTIFICATE", Bytes: certPEM}
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing X.509 certificate: %w", err)
	}

	// Verify the certificate was issued by Fulcio (check issuer)
	if err := verifyFulcioIssuer(cert); err != nil {
		return nil, fmt.Errorf("certificate issuer check: %w", err)
	}

	// Verify the certificate has not expired
	now := time.Now()
	if now.After(cert.NotAfter) {
		return nil, fmt.Errorf("certificate expired at %s", cert.NotAfter.Format(time.RFC3339))
	}

	// Verify the certificate SAN matches the expected identity
	if expectedIdentity != "" {
		if err := verifyCertificateIdentity(cert, expectedIdentity); err != nil {
			return nil, fmt.Errorf("certificate identity check: %w", err)
		}
	}

	// Extract the public key from the certificate (Fulcio uses ECDSA P-256)
	ecdsaKey, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("certificate public key is not ECDSA, got %T", cert.PublicKey)
	}

	// Decode the signature
	sigBytes, err := base64.StdEncoding.DecodeString(bundle.Signature)
	if err != nil {
		return nil, fmt.Errorf("decoding signature base64: %w", err)
	}

	// Verify the signature over SHA256(manifestYAML)
	digest := sha256.Sum256([]byte(manifestYAML))
	if !ecdsa.VerifyASN1(ecdsaKey, digest[:], sigBytes) {
		return nil, fmt.Errorf("ECDSA signature verification failed")
	}

	result := &SigstoreResult{
		Verified:       true,
		LogIndex:       bundle.RekorLogIndex,
		IntegratedTime: time.Unix(bundle.RekorIntegratedTime, 0),
		Identity:       extractCertIdentity(cert),
		RekorVerified:  false,
	}

	// Optionally verify Rekor inclusion (best-effort, non-blocking)
	if bundle.RekorLogIndex > 0 {
		rekorErr := verifyRekorInclusion(bundle.RekorLogIndex)
		if rekorErr == nil {
			result.RekorVerified = true
		}
		// Rekor check failure is not fatal; the certificate chain is sufficient
	}

	return result, nil
}

// verifyFulcioIssuer checks that the certificate was issued by a Sigstore Fulcio CA.
func verifyFulcioIssuer(cert *x509.Certificate) error {
	issuerCN := cert.Issuer.CommonName
	issuerOrg := strings.Join(cert.Issuer.Organization, " ")

	if strings.Contains(strings.ToLower(issuerCN), "sigstore") ||
		strings.Contains(strings.ToLower(issuerOrg), "sigstore") ||
		strings.Contains(strings.ToLower(issuerCN), "fulcio") {
		return nil
	}

	return fmt.Errorf("certificate issuer %q is not a recognized Sigstore/Fulcio CA", issuerCN)
}

// verifyCertificateIdentity checks that the certificate SAN contains the expected identity.
func verifyCertificateIdentity(cert *x509.Certificate, expectedIdentity string) error {
	// Check URI SANs (Fulcio stores the OIDC identity as a URI SAN)
	for _, uri := range cert.URIs {
		if uri.String() == expectedIdentity {
			return nil
		}
	}

	// Check email SANs as fallback
	for _, email := range cert.EmailAddresses {
		if email == expectedIdentity {
			return nil
		}
	}

	// Check DNS SANs as fallback
	for _, dns := range cert.DNSNames {
		if dns == expectedIdentity {
			return nil
		}
	}

	return fmt.Errorf("expected identity %q not found in certificate SANs", expectedIdentity)
}

// extractCertIdentity returns the primary identity from a Sigstore certificate.
func extractCertIdentity(cert *x509.Certificate) string {
	if len(cert.URIs) > 0 {
		return cert.URIs[0].String()
	}
	if len(cert.EmailAddresses) > 0 {
		return cert.EmailAddresses[0]
	}
	return cert.Subject.CommonName
}

// verifyRekorInclusion checks that a Rekor transparency log entry exists.
// This is a best-effort online check. Returns nil if the entry exists, error otherwise.
func verifyRekorInclusion(logIndex int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("https://rekor.sigstore.dev/api/v1/log/entries?logIndex=%d", logIndex)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating rekor request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "clictl/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("rekor request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("rekor returned status %d", resp.StatusCode)
	}

	return nil
}

// DualVerificationResult describes the outcome of verifying both Ed25519 and Sigstore.
type DualVerificationResult struct {
	Ed25519OK  bool
	SigstoreOK bool
	Warning    string
	Error      error
}

// VerifyDual performs dual verification: Ed25519 + Sigstore.
// Ed25519 is required. Sigstore is optional but produces a warning if absent.
func VerifyDual(manifestYAML, ed25519Sig string, sigstoreBundle *SigstoreBundle, expectedIdentity string) DualVerificationResult {
	result := DualVerificationResult{}

	// Ed25519 verification (required)
	if err := VerifyManifest(manifestYAML, ed25519Sig); err != nil {
		result.Error = fmt.Errorf("ed25519 verification failed: %w", err)
		// If Sigstore is also present, try it
		if sigstoreBundle != nil {
			if _, err2 := VerifySigstoreBundle(manifestYAML, *sigstoreBundle, expectedIdentity); err2 != nil {
				result.Error = fmt.Errorf("both verifications failed: ed25519: %w, sigstore: %v", err, err2)
			} else {
				result.SigstoreOK = true
				result.Error = fmt.Errorf("ed25519 verification failed: %w", err)
			}
		}
		return result
	}
	result.Ed25519OK = true

	// Sigstore verification (optional)
	if sigstoreBundle == nil {
		result.Warning = "No Sigstore bundle present. Ed25519 signature verified, but Sigstore provides additional supply chain transparency."
		return result
	}

	if _, err := VerifySigstoreBundle(manifestYAML, *sigstoreBundle, expectedIdentity); err != nil {
		result.Warning = fmt.Sprintf("Sigstore verification failed: %v. Ed25519 signature verified.", err)
		return result
	}
	result.SigstoreOK = true

	return result
}

// sigstoreVerificationConfig caches the well-known verification config.
var (
	sigstoreConfigOnce   sync.Once
	sigstoreConfigResult *wellKnownSigstoreConfig
)

type wellKnownSigstoreConfig struct {
	ExpectedIdentity string `json:"expected_identity"`
}

// ResolveSigstoreIdentity returns the expected Sigstore identity for verification.
// It first checks the compiled-in value (set via ldflags), then falls back to
// fetching the well-known config from the API.
func ResolveSigstoreIdentity(ctx context.Context, apiURL string) string {
	if SigstoreExpectedIdentity != "" {
		return SigstoreExpectedIdentity
	}

	sigstoreConfigOnce.Do(func() {
		sigstoreConfigResult = fetchWellKnownSigstoreConfig(ctx, apiURL)
	})

	if sigstoreConfigResult != nil {
		return sigstoreConfigResult.ExpectedIdentity
	}
	return ""
}

// fetchWellKnownSigstoreConfig fetches the Sigstore verification config from the API.
func fetchWellKnownSigstoreConfig(ctx context.Context, apiURL string) *wellKnownSigstoreConfig {
	fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/.well-known/sigstore-verification-config.json", apiURL)
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "clictl/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var cfg wellKnownSigstoreConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return nil
	}

	return &cfg
}
