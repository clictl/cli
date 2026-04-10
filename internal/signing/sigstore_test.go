// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package signing

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"net/url"
	"strings"
	"testing"
	"time"
)

// generateTestSigstoreBundle creates a test Sigstore bundle with a self-signed certificate
// that mimics a Fulcio-issued cert (Sigstore issuer, URI SAN for identity).
func generateTestSigstoreBundle(t *testing.T, manifest string, identity string) (SigstoreBundle, *ecdsa.PrivateKey) {
	t.Helper()
	return generateTestSigstoreBundleWithExpiry(t, manifest, identity, time.Now().Add(1*time.Hour))
}

// generateTestSigstoreBundleWithExpiry creates a bundle with a configurable NotAfter.
func generateTestSigstoreBundleWithExpiry(t *testing.T, manifest string, identity string, notAfter time.Time) (SigstoreBundle, *ecdsa.PrivateKey) {
	t.Helper()

	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating ECDSA key: %v", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generating serial number: %v", err)
	}

	identityURL, err := url.Parse(identity)
	if err != nil {
		t.Fatalf("parsing identity URL: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Issuer: pkix.Name{
			CommonName:   "sigstore-intermediate",
			Organization: []string{"sigstore.dev"},
		},
		Subject: pkix.Name{
			CommonName: "sigstore",
		},
		NotBefore: time.Now().Add(-1 * time.Hour),
		NotAfter:  notAfter,
		URIs:      []*url.URL{identityURL},
		KeyUsage:  x509.KeyUsageDigitalSignature,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privKey.PublicKey, privKey)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	certB64 := base64.StdEncoding.EncodeToString(certPEM)

	digest := sha256.Sum256([]byte(manifest))
	sigBytes, err := ecdsa.SignASN1(rand.Reader, privKey, digest[:])
	if err != nil {
		t.Fatalf("signing manifest: %v", err)
	}
	sigB64 := base64.StdEncoding.EncodeToString(sigBytes)

	bundle := SigstoreBundle{
		Certificate:         certB64,
		Signature:           sigB64,
		RekorLogIndex:       12345,
		RekorLogID:          "c0d23d6ad406973f9559f3ba2d1ca01f84147d8ffc5b8445c224f98b9591801d",
		RekorIntegratedTime: time.Now().Unix(),
	}

	return bundle, privKey
}

// ---------------------------------------------------------------------------
// Sigstore bundle parsing tests
// ---------------------------------------------------------------------------

func TestParseSigstoreBundle_Valid(t *testing.T) {
	input := `{
		"certificate": "dGVzdC1jZXJ0",
		"signature": "dGVzdC1zaWc=",
		"rekor_log_index": 42,
		"rekor_log_id": "abc123",
		"rekor_integrated_time": 1711800000
	}`

	bundle, err := ParseSigstoreBundle([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if bundle.Certificate != "dGVzdC1jZXJ0" {
		t.Errorf("certificate = %q, want %q", bundle.Certificate, "dGVzdC1jZXJ0")
	}
	if bundle.Signature != "dGVzdC1zaWc=" {
		t.Errorf("signature = %q, want %q", bundle.Signature, "dGVzdC1zaWc=")
	}
	if bundle.RekorLogIndex != 42 {
		t.Errorf("rekor_log_index = %d, want 42", bundle.RekorLogIndex)
	}
	if bundle.RekorLogID != "abc123" {
		t.Errorf("rekor_log_id = %q, want %q", bundle.RekorLogID, "abc123")
	}
	if bundle.RekorIntegratedTime != 1711800000 {
		t.Errorf("rekor_integrated_time = %d, want 1711800000", bundle.RekorIntegratedTime)
	}
}

func TestParseSigstoreBundle_Invalid(t *testing.T) {
	_, err := ParseSigstoreBundle([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestParseSigstoreBundle_MissingFields(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"missing certificate", `{"signature": "dGVzdA=="}`},
		{"missing signature", `{"certificate": "dGVzdA=="}`},
		{"both missing", `{}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSigstoreBundle([]byte(tc.input))
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Sigstore bundle verification tests
// ---------------------------------------------------------------------------

func TestVerifySigstoreBundle_ValidSignature(t *testing.T) {
	manifest := "name: test-skill\nversion: 1.0.0\n"
	identity := "https://github.com/clictl/registry/.github/workflows/build.yml@refs/heads/main"

	bundle, _ := generateTestSigstoreBundle(t, manifest, identity)

	result, err := VerifySigstoreBundle(manifest, bundle, identity)
	if err != nil {
		t.Fatalf("expected valid verification, got error: %v", err)
	}
	if !result.Verified {
		t.Error("expected result.Verified to be true")
	}
	if result.Identity != identity {
		t.Errorf("identity = %q, want %q", result.Identity, identity)
	}
	if result.LogIndex != 12345 {
		t.Errorf("log index = %d, want 12345", result.LogIndex)
	}
}

func TestVerifySigstoreBundle_TamperedManifest(t *testing.T) {
	manifest := "name: test-skill\nversion: 1.0.0\n"
	identity := "https://github.com/clictl/registry/.github/workflows/build.yml@refs/heads/main"

	bundle, _ := generateTestSigstoreBundle(t, manifest, identity)

	tampered := "name: evil-skill\nversion: 1.0.0\n"
	_, err := VerifySigstoreBundle(tampered, bundle, identity)
	if err == nil {
		t.Fatal("expected error for tampered manifest, got nil")
	}
	if !strings.Contains(err.Error(), "ECDSA signature verification failed") {
		t.Errorf("expected ECDSA verification error, got: %v", err)
	}
}

func TestVerifySigstoreBundle_ExpiredCertificate(t *testing.T) {
	manifest := "name: test-skill\nversion: 1.0.0\n"
	identity := "https://github.com/clictl/registry/.github/workflows/build.yml@refs/heads/main"

	// Generate a certificate that expired 1 hour ago
	expiredTime := time.Now().Add(-1 * time.Hour)
	bundle, _ := generateTestSigstoreBundleWithExpiry(t, manifest, identity, expiredTime)

	_, err := VerifySigstoreBundle(manifest, bundle, identity)
	if err == nil {
		t.Fatal("expected error for expired certificate, got nil")
	}
	// The error might come from certificate expiry check or from x509 parse.
	// Either way, verification should not succeed.
}

func TestVerifySigstoreBundle_WrongIdentity(t *testing.T) {
	manifest := "name: test-skill\nversion: 1.0.0\n"
	identity := "https://github.com/clictl/registry/.github/workflows/build.yml@refs/heads/main"

	bundle, _ := generateTestSigstoreBundle(t, manifest, identity)

	wrongIdentity := "https://github.com/evil/repo/.github/workflows/build.yml@refs/heads/main"
	_, err := VerifySigstoreBundle(manifest, bundle, wrongIdentity)
	if err == nil {
		t.Fatal("expected error for wrong identity, got nil")
	}
	if !strings.Contains(err.Error(), "identity") {
		t.Errorf("expected identity error, got: %v", err)
	}
}

func TestVerifySigstoreBundle_InvalidCertificate(t *testing.T) {
	garbageCert := base64.StdEncoding.EncodeToString([]byte("not a certificate"))
	bundle := SigstoreBundle{
		Certificate: garbageCert,
		Signature:   "dGVzdA==",
	}

	_, err := VerifySigstoreBundle("name: test\n", bundle, "")
	if err == nil {
		t.Fatal("expected error for invalid certificate, got nil")
	}
}

func TestVerifySigstoreBundle_InvalidSignature(t *testing.T) {
	manifest := "name: test-skill\nversion: 1.0.0\n"
	identity := "https://github.com/clictl/registry/.github/workflows/build.yml@refs/heads/main"

	// Generate a valid cert but with garbage signature
	bundle, _ := generateTestSigstoreBundle(t, manifest, identity)
	bundle.Signature = "!!!not-valid-base64!!!"

	_, err := VerifySigstoreBundle(manifest, bundle, identity)
	if err == nil {
		t.Fatal("expected error for invalid base64 signature, got nil")
	}
	if !strings.Contains(err.Error(), "decoding signature") {
		t.Errorf("expected signature decoding error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Dual verification tests
// ---------------------------------------------------------------------------

func TestDualVerification_BothPass(t *testing.T) {
	manifest := "name: dual-test\nversion: 1.0.0\n"

	// Set up Ed25519 key
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	old := RegistryPublicKeyB64
	RegistryPublicKeyB64 = base64.StdEncoding.EncodeToString(pub)
	defer func() { RegistryPublicKeyB64 = old }()

	hash := sha256.Sum256([]byte(manifest))
	ed25519Sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, hash[:]))

	// Set up Sigstore bundle
	identity := "https://github.com/clictl/registry/.github/workflows/build.yml@refs/heads/main"
	sigstoreBundle, _ := generateTestSigstoreBundle(t, manifest, identity)

	result := VerifyDual(manifest, ed25519Sig, &sigstoreBundle, identity)

	if !result.Ed25519OK {
		t.Error("expected Ed25519OK to be true")
	}
	if !result.SigstoreOK {
		t.Error("expected SigstoreOK to be true")
	}
	if result.Error != nil {
		t.Errorf("expected no error, got: %v", result.Error)
	}
	if result.Warning != "" {
		t.Errorf("expected no warning, got: %q", result.Warning)
	}
}

func TestDualVerification_Ed25519OnlySigstoreMissing(t *testing.T) {
	manifest := "name: ed25519-only\nversion: 1.0.0\n"

	// Set up Ed25519 key
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	old := RegistryPublicKeyB64
	RegistryPublicKeyB64 = base64.StdEncoding.EncodeToString(pub)
	defer func() { RegistryPublicKeyB64 = old }()

	hash := sha256.Sum256([]byte(manifest))
	ed25519Sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, hash[:]))

	// No Sigstore bundle
	result := VerifyDual(manifest, ed25519Sig, nil, "")

	if !result.Ed25519OK {
		t.Error("expected Ed25519OK to be true")
	}
	if result.SigstoreOK {
		t.Error("expected SigstoreOK to be false (no bundle)")
	}
	if result.Error != nil {
		t.Errorf("expected no error, got: %v", result.Error)
	}
	if result.Warning == "" {
		t.Error("expected a warning about missing Sigstore bundle")
	}
	if !strings.Contains(result.Warning, "No Sigstore bundle") {
		t.Errorf("unexpected warning text: %q", result.Warning)
	}
}

func TestDualVerification_BothFail(t *testing.T) {
	manifest := "name: both-fail\nversion: 1.0.0\n"

	// Use wrong Ed25519 key
	old := RegistryPublicKeyB64
	wrongPub, _, _ := ed25519.GenerateKey(nil)
	RegistryPublicKeyB64 = base64.StdEncoding.EncodeToString(wrongPub)
	defer func() { RegistryPublicKeyB64 = old }()

	// Sign with a different key
	_, realPriv, _ := ed25519.GenerateKey(nil)
	hash := sha256.Sum256([]byte(manifest))
	ed25519Sig := base64.StdEncoding.EncodeToString(ed25519.Sign(realPriv, hash[:]))

	// Create a Sigstore bundle with wrong identity
	identity := "https://github.com/clictl/registry/.github/workflows/build.yml@refs/heads/main"
	sigstoreBundle, _ := generateTestSigstoreBundle(t, manifest, identity)

	// Verify with wrong expected identity to make Sigstore fail too
	result := VerifyDual(manifest, ed25519Sig, &sigstoreBundle, "https://github.com/wrong/identity")

	if result.Ed25519OK {
		t.Error("expected Ed25519OK to be false")
	}
	if result.SigstoreOK {
		t.Error("expected SigstoreOK to be false")
	}
	if result.Error == nil {
		t.Error("expected error when both verifications fail")
	}
	if !strings.Contains(result.Error.Error(), "both") {
		t.Errorf("expected 'both' in error message, got: %v", result.Error)
	}
}

// ---------------------------------------------------------------------------
// Additional Sigstore coverage tests
// ---------------------------------------------------------------------------

func TestVerifySigstoreBundle_NoIdentityCheck(t *testing.T) {
	manifest := "name: test-skill\nversion: 1.0.0\n"
	identity := "https://github.com/clictl/registry/.github/workflows/build.yml@refs/heads/main"

	bundle, _ := generateTestSigstoreBundle(t, manifest, identity)

	// Passing empty expectedIdentity should skip the identity check
	result, err := VerifySigstoreBundle(manifest, bundle, "")
	if err != nil {
		t.Fatalf("expected valid verification with empty identity, got error: %v", err)
	}
	if !result.Verified {
		t.Error("expected result.Verified to be true")
	}
}

func TestVerifySigstoreBundle_NonFulcioCert(t *testing.T) {
	manifest := "name: test-skill\nversion: 1.0.0\n"

	// Generate a certificate that does NOT have Sigstore/Fulcio issuer
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Issuer: pkix.Name{
			CommonName:   "Evil Corp CA",
			Organization: []string{"Evil Corp"},
		},
		Subject: pkix.Name{
			CommonName: "evil",
		},
		NotBefore: time.Now().Add(-1 * time.Hour),
		NotAfter:  time.Now().Add(1 * time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privKey.PublicKey, privKey)
	if err != nil {
		t.Fatal(err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	certB64 := base64.StdEncoding.EncodeToString(certPEM)

	digestBytes := sha256.Sum256([]byte(manifest))
	sigBytes, _ := ecdsa.SignASN1(rand.Reader, privKey, digestBytes[:])
	sigB64 := base64.StdEncoding.EncodeToString(sigBytes)

	bundle := SigstoreBundle{
		Certificate: certB64,
		Signature:   sigB64,
	}

	_, err = VerifySigstoreBundle(manifest, bundle, "")
	if err == nil {
		t.Fatal("expected error for non-Fulcio certificate, got nil")
	}
}

// ---------------------------------------------------------------------------
// Ed25519 manifest verification tests (merged from verify_test.go)
// ---------------------------------------------------------------------------

func TestVerifyManifest_ValidSignature(t *testing.T) {
	// Generate test keypair
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	manifest := "name: test-skill\nversion: 1.0.0\n"
	hash := sha256.Sum256([]byte(manifest))
	sig := ed25519.Sign(priv, hash[:])

	pubB64 := base64.StdEncoding.EncodeToString(pub)
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	err = VerifyManifestWithKey(manifest, sigB64, pubB64)
	if err != nil {
		t.Fatalf("expected valid signature, got error: %v", err)
	}
}

func TestVerifyManifest_TamperedManifest(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)

	manifest := "name: test-skill\nversion: 1.0.0\n"
	hash := sha256.Sum256([]byte(manifest))
	sig := ed25519.Sign(priv, hash[:])

	pubB64 := base64.StdEncoding.EncodeToString(pub)
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	// Tamper with manifest
	tampered := "name: evil-skill\nversion: 1.0.0\n"
	err := VerifyManifestWithKey(tampered, sigB64, pubB64)
	if err == nil {
		t.Fatal("expected error for tampered manifest, got nil")
	}
}

func TestVerifyManifest_WrongKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	otherPub, _, _ := ed25519.GenerateKey(nil)

	manifest := "name: test-skill\nversion: 1.0.0\n"
	hash := sha256.Sum256([]byte(manifest))
	sig := ed25519.Sign(priv, hash[:])

	otherPubB64 := base64.StdEncoding.EncodeToString(otherPub)
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	err := VerifyManifestWithKey(manifest, sigB64, otherPubB64)
	if err == nil {
		t.Fatal("expected error for wrong key, got nil")
	}
}

func TestVerifyManifest_NoPublicKey(t *testing.T) {
	old := RegistryPublicKeyB64
	RegistryPublicKeyB64 = ""
	defer func() { RegistryPublicKeyB64 = old }()

	err := VerifyManifest("test", "dGVzdA==")
	if err == nil {
		t.Fatal("expected error when no public key configured")
	}
}
