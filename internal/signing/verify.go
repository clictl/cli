// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package signing

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// RegistryPublicKeyB64 is the Ed25519 public key for the clictl.dev registry.
// Override via ldflags: -X github.com/clictl/cli/internal/signing.RegistryPublicKeyB64=...
var RegistryPublicKeyB64 = "QwghfjOnq537SHpEdw+rYGwZGiN6AtbNJK7jgU2ydl4="

// VerifyManifest verifies an Ed25519 signature on a manifest YAML string.
// Returns nil if valid, error if invalid or verification fails.
func VerifyManifest(manifestYAML string, signatureB64 string) error {
	if RegistryPublicKeyB64 == "" {
		return fmt.Errorf("no registry public key configured")
	}

	pubKeyBytes, err := base64.StdEncoding.DecodeString(RegistryPublicKeyB64)
	if err != nil {
		return fmt.Errorf("decoding public key: %w", err)
	}

	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key size: got %d, want %d", len(pubKeyBytes), ed25519.PublicKeySize)
	}

	sig, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return fmt.Errorf("decoding signature: %w", err)
	}

	hash := sha256.Sum256([]byte(manifestYAML))
	pubKey := ed25519.PublicKey(pubKeyBytes)

	if !ed25519.Verify(pubKey, hash[:], sig) {
		return fmt.Errorf("signature verification failed")
	}

	return nil
}

// VerifyManifestWithKey verifies using a specific public key (for testing).
func VerifyManifestWithKey(manifestYAML string, signatureB64 string, publicKeyB64 string) error {
	pubKeyBytes, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil {
		return fmt.Errorf("decoding public key: %w", err)
	}

	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key size: got %d, want %d", len(pubKeyBytes), ed25519.PublicKeySize)
	}

	sig, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return fmt.Errorf("decoding signature: %w", err)
	}

	hash := sha256.Sum256([]byte(manifestYAML))
	pubKey := ed25519.PublicKey(pubKeyBytes)

	if !ed25519.Verify(pubKey, hash[:], sig) {
		return fmt.Errorf("signature verification failed")
	}

	return nil
}
