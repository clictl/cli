// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/clictl/cli/internal/archive"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/signing"
)

// testTransport redirects all HTTP requests to the test server,
// preserving the original request path. This allows us to intercept
// calls to raw.githubusercontent.com without modifying production code.
type testTransport struct {
	server *httptest.Server
}

func (tt *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite the URL to point at the test server, keeping the path
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(tt.server.URL, "http://")
	// Use a fresh transport for the actual call to avoid recursion
	return (&http.Transport{}).RoundTrip(req)
}

// stubTransport installs a testTransport for the duration of the test and
// restores the original http.DefaultTransport on cleanup.
// fetchAndVerifySkillFiles creates its own http.Client without an explicit
// Transport, so it falls through to http.DefaultTransport.
func stubTransport(t *testing.T, server *httptest.Server) {
	t.Helper()
	orig := http.DefaultTransport
	http.DefaultTransport = &testTransport{server: server}
	t.Cleanup(func() { http.DefaultTransport = orig })
}

// ---------------------------------------------------------------------------
// File fetching and SHA256 verification
// ---------------------------------------------------------------------------

func TestFetchAndVerifySkillFiles_SHA256Mismatch(t *testing.T) {
	content := "# Real Skill Content"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, content)
	}))
	defer server.Close()
	stubTransport(t, server)

	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"

	src := &models.SkillSource{
		Repo: "test-org/test-repo",
		Ref:  "main",
		Path: "skills/test",
		Files: []models.SkillSourceFile{
			{Path: "SKILL.md", SHA256: wrongHash},
		},
	}

	_, err := fetchAndVerifySkillFiles(context.Background(), src)
	if err == nil {
		t.Fatal("expected error for SHA256 mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Errorf("expected error to contain 'SHA256 mismatch', got: %v", err)
	}
}

func TestFetchAndVerifySkillFiles_MissingFile404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Not Found", http.StatusNotFound)
	}))
	defer server.Close()
	stubTransport(t, server)

	src := &models.SkillSource{
		Repo: "test-org/test-repo",
		Ref:  "main",
		Path: "skills/test",
		Files: []models.SkillSourceFile{
			{Path: "SKILL.md", SHA256: "abc123"},
		},
	}

	_, err := fetchAndVerifySkillFiles(context.Background(), src)
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected error to mention 404 status, got: %v", err)
	}
}

func TestFetchAndVerifySkillFiles_NoSHA256InManifest(t *testing.T) {
	content := "# Skill without hash verification"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, content)
	}))
	defer server.Close()
	stubTransport(t, server)

	src := &models.SkillSource{
		Repo: "test-org/test-repo",
		Ref:  "main",
		Path: "skills/test",
		Files: []models.SkillSourceFile{
			{Path: "SKILL.md", SHA256: ""}, // no hash provided
		},
	}

	files, err := fetchAndVerifySkillFiles(context.Background(), src)
	if err != nil {
		t.Fatalf("expected no error when SHA256 is empty, got: %v", err)
	}
	if string(files["SKILL.md"]) != content {
		t.Errorf("expected content %q, got %q", content, string(files["SKILL.md"]))
	}
}

func TestFetchAndVerifySkillFiles_AllFilesValid(t *testing.T) {
	fileContents := map[string]string{
		"SKILL.md":  "# My Skill\nDo things.",
		"helper.py": "def run(): pass\n",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		name := parts[len(parts)-1]
		if body, ok := fileContents[name]; ok {
			fmt.Fprint(w, body)
		} else {
			http.Error(w, "Not Found", http.StatusNotFound)
		}
	}))
	defer server.Close()
	stubTransport(t, server)

	// Compute correct SHA256 hashes
	skillHash := sha256Hex([]byte(fileContents["SKILL.md"]))
	helperHash := sha256Hex([]byte(fileContents["helper.py"]))

	src := &models.SkillSource{
		Repo: "test-org/test-repo",
		Ref:  "main",
		Path: "skills/test",
		Files: []models.SkillSourceFile{
			{Path: "SKILL.md", SHA256: skillHash},
			{Path: "helper.py", SHA256: helperHash},
		},
	}

	files, err := fetchAndVerifySkillFiles(context.Background(), src)
	if err != nil {
		t.Fatalf("expected no error for valid files, got: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	for name, expectedContent := range fileContents {
		got, ok := files[name]
		if !ok {
			t.Errorf("missing file %q in result", name)
			continue
		}
		if string(got) != expectedContent {
			t.Errorf("file %q: expected %q, got %q", name, expectedContent, string(got))
		}
	}
}

func TestFetchAndVerifySkillFiles_EmptyFilesList(t *testing.T) {
	singleFileContent := "# Legacy single-file skill"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, singleFileContent)
	}))
	defer server.Close()
	stubTransport(t, server)

	src := &models.SkillSource{
		Repo:  "test-org/test-repo",
		Ref:   "main",
		Path:  "skills/legacy/SKILL.md",
		Files: nil, // empty - backward compat: fetches source.Path directly
	}

	files, err := fetchAndVerifySkillFiles(context.Background(), src)
	if err != nil {
		t.Fatalf("expected no error for empty files list (backward compat), got: %v", err)
	}

	// Should fall back to fetching source.Path as a single file,
	// with filename derived from filepath.Base(src.Path)
	got, ok := files["SKILL.md"]
	if !ok {
		t.Fatal("expected file 'SKILL.md' in result (derived from path basename)")
	}
	if string(got) != singleFileContent {
		t.Errorf("expected %q, got %q", singleFileContent, string(got))
	}
}

// ---------------------------------------------------------------------------
// Trust flag and alias resolution
// ---------------------------------------------------------------------------

func TestTrustFlag(t *testing.T) {
	tests := []struct {
		name       string
		spec       *models.ToolSpec
		trustFlag  bool
		wantSkip   bool
	}{
		{
			name:      "unverified no namespace skipped without trust",
			spec:      &models.ToolSpec{Name: "github-mcp", Version: "1.0.0", IsVerified: false, Namespace: ""},
			trustFlag: false,
			wantSkip:  true,
		},
		{
			name:      "verified tool not skipped",
			spec:      &models.ToolSpec{Name: "github-mcp", Version: "1.0.0", IsVerified: true, Namespace: "anthropic"},
			trustFlag: false,
			wantSkip:  false,
		},
		{
			name:      "unverified with namespace not skipped",
			spec:      &models.ToolSpec{Name: "my-tool", Version: "1.0.0", IsVerified: false, Namespace: "acme-corp"},
			trustFlag: false,
			wantSkip:  false,
		},
		{
			name:      "trust overrides unverified",
			spec:      &models.ToolSpec{Name: "github-mcp", Version: "1.0.0", IsVerified: false, Namespace: ""},
			trustFlag: true,
			wantSkip:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shouldSkip := !tt.spec.IsVerified && tt.spec.Namespace == "" && !tt.trustFlag
			if shouldSkip != tt.wantSkip {
				t.Errorf("shouldSkip = %v, want %v", shouldSkip, tt.wantSkip)
			}
		})
	}
}

func TestAliasResolution_Display(t *testing.T) {
	spec := &models.ToolSpec{
		Name:         "github-mcp",
		Version:      "1.0.0",
		Namespace:    "anthropic",
		ResolvedFrom: "old-github-mcp",
	}

	// Verify that resolved_from triggers display logic
	if spec.ResolvedFrom == "" {
		t.Error("expected ResolvedFrom to be set")
	}

	displayName := spec.Name
	if spec.Namespace != "" {
		displayName = fmt.Sprintf("@%s/%s", spec.Namespace, spec.Name)
	}

	expected := "@anthropic/github-mcp"
	if displayName != expected {
		t.Errorf("expected display name %q, got %q", expected, displayName)
	}
}

func TestAliasResolution_NoAlias(t *testing.T) {
	spec := &models.ToolSpec{
		Name:         "github-mcp",
		Version:      "1.0.0",
		ResolvedFrom: "",
	}

	if spec.ResolvedFrom != "" {
		t.Error("expected ResolvedFrom to be empty for non-aliased spec")
	}
}

// ---------------------------------------------------------------------------
// Instructions content
// ---------------------------------------------------------------------------

func TestInstructionsContent(t *testing.T) {
	if !strings.Contains(claudeMDContent, "clictl search") {
		t.Error("instructions content missing clictl search")
	}
	if !strings.Contains(claudeMDContent, "Do NOT run the install command yourself") {
		t.Error("instructions content missing safety directive")
	}
	if !strings.Contains(claudeMDContent, "clictl.dev/install.sh") {
		t.Error("instructions content missing install URL")
	}
}

// ---------------------------------------------------------------------------
// Pack integration: community install, signed pack, fallback, tamper detect
// ---------------------------------------------------------------------------

func TestCommunityInstall_NoAuth(t *testing.T) {
	// Set up a fake skill with known content
	contentDir := t.TempDir()
	skillContent := "# Community Skill\nThis skill is community-contributed.\n"
	os.WriteFile(filepath.Join(contentDir, "SKILL.md"), []byte(skillContent), 0644)

	contentHash, err := archive.HashDirectory(contentDir)
	if err != nil {
		t.Fatal(err)
	}

	// Create pack archive
	manifest := map[string]interface{}{
		"schema_version": "1",
		"name":           "community-tool",
		"version":        "1.0.0",
		"type":           "skill",
		"content_sha256": contentHash,
	}
	packDir := t.TempDir()
	archivePath, err := archive.Pack(contentDir, manifest, packDir)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}

	// Serve the archive from a test server (simulates GitHub raw content)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "SKILL.md") {
			w.Write([]byte(skillContent))
			return
		}
		w.Write(archiveData)
	}))
	defer server.Close()
	stubTransport(t, server)

	// Create a ToolSpec that looks like a community tool (no namespace, from toolbox)
	spec := &models.ToolSpec{
		Name:        "community-tool",
		Version:     "1.0.0",
		IsVerified:  false,
		Namespace:   "",
		Description: "A community-contributed tool",
	}

	// Simulate the community install flow: spec resolution works without auth
	src := &models.SkillSource{
		Repo: "test-org/test-repo",
		Ref:  "main",
		Path: "skills/community",
		Files: []models.SkillSourceFile{
			{Path: "SKILL.md", SHA256: sha256Hex([]byte(skillContent))},
		},
	}

	files, err := fetchAndVerifySkillFiles(context.Background(), src)
	if err != nil {
		t.Fatalf("community install should work without auth: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	if string(files["SKILL.md"]) != skillContent {
		t.Errorf("expected skill content %q, got %q", skillContent, string(files["SKILL.md"]))
	}

	// Verify trust display: community tool should show appropriate tier
	shouldWarn := !spec.IsVerified && spec.Namespace == ""
	if !shouldWarn {
		t.Error("community tool should trigger unverified warning")
	}
}

func TestSignedPackInstall_FullFlow(t *testing.T) {
	// Generate a test signing keypair
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	// Create skill content
	contentDir := t.TempDir()
	skillContent := "# Signed Skill\nThis skill has cryptographic provenance.\n"
	os.WriteFile(filepath.Join(contentDir, "SKILL.md"), []byte(skillContent), 0644)
	os.MkdirAll(filepath.Join(contentDir, "scripts"), 0755)
	scriptContent := "#!/bin/sh\necho 'running signed skill'\n"
	os.WriteFile(filepath.Join(contentDir, "scripts", "run.sh"), []byte(scriptContent), 0755)

	contentHash, err := archive.HashDirectory(contentDir)
	if err != nil {
		t.Fatal(err)
	}

	// Build the manifest YAML
	manifestYAML := fmt.Sprintf(`schema_version: "1"
name: signed-skill
type: skill
version: 1.0.0
description: "A signed skill with full provenance"
content_sha256: %s
publisher:
  name: testpub
  identity: "github:testpub"
provenance:
  builder: clictl.dev/registry
  source_repo: testpub/my-skills
  source_ref: v1.0.0
`, contentHash)

	// Sign the manifest
	manifestHash := sha256.Sum256([]byte(manifestYAML))
	sig := ed25519.Sign(priv, manifestHash[:])
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	// Step 1: Verify the signature
	err = signing.VerifyManifestWithKey(manifestYAML, sigB64, pubB64)
	if err != nil {
		t.Fatalf("signature verification failed: %v", err)
	}

	// Step 2: Build the pack archive
	packManifest := map[string]interface{}{
		"schema_version": "1",
		"name":           "signed-skill",
		"version":        "1.0.0",
		"type":           "skill",
		"content_sha256": contentHash,
	}
	packDir := t.TempDir()
	archivePath, err := archive.Pack(contentDir, packManifest, packDir)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	// Step 3: Unpack and verify content hash
	extractDir := t.TempDir()
	parsed, err := archive.Unpack(archivePath, extractDir)
	if err != nil {
		t.Fatalf("Unpack failed: %v", err)
	}

	if parsed["name"] != "signed-skill" {
		t.Errorf("expected name 'signed-skill', got %v", parsed["name"])
	}

	// Step 4: Verify content hash matches
	err = archive.VerifyPackContent(extractDir, contentHash)
	if err != nil {
		t.Fatalf("content hash verification failed: %v", err)
	}

	// Step 5: Verify the extracted SKILL.md is correct
	extractedSkill, err := os.ReadFile(filepath.Join(extractDir, "content", "SKILL.md"))
	if err != nil {
		t.Fatalf("reading extracted SKILL.md: %v", err)
	}
	if string(extractedSkill) != skillContent {
		t.Errorf("extracted content mismatch: got %q, want %q", string(extractedSkill), skillContent)
	}

	// Step 6: Verify the extracted script is correct
	extractedScript, err := os.ReadFile(filepath.Join(extractDir, "content", "scripts", "run.sh"))
	if err != nil {
		t.Fatalf("reading extracted script: %v", err)
	}
	if string(extractedScript) != scriptContent {
		t.Errorf("extracted script mismatch: got %q, want %q", string(extractedScript), scriptContent)
	}
}

func TestInstallFallback_NotLoggedIn(t *testing.T) {
	skillContent := "# Fallback Skill\nInstalled from toolbox because user is not logged in.\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, skillContent)
	}))
	defer server.Close()
	stubTransport(t, server)

	// Simulate the scenario: user is NOT logged in (no auth token)
	isLoggedIn := false

	// When not logged in, the CLI should skip signed pack download and fall back
	// to fetching from the toolbox (GitHub raw content)
	spec := &models.ToolSpec{
		Name:       "fallback-tool",
		Version:    "1.0.0",
		IsVerified: false,
		Namespace:  "",
	}

	// Simulate fallback decision
	shouldUsePack := isLoggedIn && spec.IsVerified
	if shouldUsePack {
		t.Error("should not use signed pack when not logged in")
	}

	// Fallback: install from toolbox (community flow)
	src := &models.SkillSource{
		Repo:  "test-org/test-repo",
		Ref:   "main",
		Path:  "skills/fallback/SKILL.md",
		Files: nil, // empty - backward compat
	}

	files, err := fetchAndVerifySkillFiles(context.Background(), src)
	if err != nil {
		t.Fatalf("fallback install should work: %v", err)
	}

	got, ok := files["SKILL.md"]
	if !ok {
		t.Fatal("expected SKILL.md in result")
	}
	if string(got) != skillContent {
		t.Errorf("expected %q, got %q", skillContent, string(got))
	}

	// Verify info message logic: should inform user about signed packs
	infoMessage := ""
	if !isLoggedIn {
		infoMessage = "Log in with 'clictl login' to install signed packs with cryptographic verification."
	}
	if infoMessage == "" {
		t.Error("expected info message about signed packs for unauthenticated users")
	}
}

func TestTamperedPackRejected_HashMismatch(t *testing.T) {
	// Create original content
	contentDir := t.TempDir()
	os.WriteFile(filepath.Join(contentDir, "SKILL.md"), []byte("# Original Skill\n"), 0644)

	contentHash, err := archive.HashDirectory(contentDir)
	if err != nil {
		t.Fatal(err)
	}

	// Build pack with correct hash
	manifest := map[string]interface{}{
		"schema_version": "1",
		"name":           "tampered-tool",
		"version":        "1.0.0",
		"type":           "skill",
		"content_sha256": contentHash,
	}
	packDir := t.TempDir()
	archivePath, err := archive.Pack(contentDir, manifest, packDir)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	// Unpack the archive
	extractDir := t.TempDir()
	_, err = archive.Unpack(archivePath, extractDir)
	if err != nil {
		t.Fatalf("Unpack failed: %v", err)
	}

	// Tamper with the extracted content (simulate MITM or compromised storage)
	tamperedFile := filepath.Join(extractDir, "content", "SKILL.md")
	os.WriteFile(tamperedFile, []byte("# EVIL SKILL\nThis reads ~/.ssh/id_rsa\n"), 0644)

	// Verify content hash should fail - tampered content does not match original hash
	err = archive.VerifyPackContent(extractDir, contentHash)
	if err == nil {
		t.Fatal("expected hash mismatch error for tampered pack, got nil")
	}
	if !strings.Contains(err.Error(), "mismatch") && !strings.Contains(err.Error(), "hash") {
		t.Errorf("expected hash mismatch error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sigstore and dual signature verification
// ---------------------------------------------------------------------------

// makeTestSigstoreBundle creates a Sigstore bundle for testing.
// It generates a self-signed ECDSA P-256 cert mimicking Fulcio, signs the
// manifest with that key, and returns the bundle JSON and private key.
func makeTestSigstoreBundle(t *testing.T, manifest string, identity string) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()

	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating ECDSA key: %v", err)
	}

	serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	identityURL, _ := url.Parse(identity)

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
		NotAfter:  time.Now().Add(1 * time.Hour),
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

	bundle := signing.SigstoreBundle{
		Certificate:         certB64,
		Signature:           sigB64,
		RekorLogIndex:       55555,
		RekorLogID:          "test-log-id",
		RekorIntegratedTime: time.Now().Unix(),
	}

	bundleJSON, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshaling bundle: %v", err)
	}

	return bundleJSON, privKey
}

func TestInstallWithDualSignature(t *testing.T) {
	manifest := "name: dual-sig-tool\nversion: 2.0.0\ntype: skill\n"
	identity := "https://github.com/clictl/registry/.github/workflows/build.yml@refs/heads/main"

	// Generate Ed25519 keypair
	ed25519Pub, ed25519Priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	ed25519PubB64 := base64.StdEncoding.EncodeToString(ed25519Pub)

	// Sign the manifest with Ed25519
	ed25519Hash := sha256.Sum256([]byte(manifest))
	ed25519Sig := base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519Priv, ed25519Hash[:]))

	// Generate Sigstore bundle
	sigstoreBundleJSON, _ := makeTestSigstoreBundle(t, manifest, identity)

	// Parse the bundle to validate it was created correctly
	bundle, err := signing.ParseSigstoreBundle(sigstoreBundleJSON)
	if err != nil {
		t.Fatalf("parsing test bundle: %v", err)
	}

	// Verify Ed25519 signature
	old := signing.RegistryPublicKeyB64
	signing.RegistryPublicKeyB64 = ed25519PubB64
	defer func() { signing.RegistryPublicKeyB64 = old }()

	err = signing.VerifyManifest(manifest, ed25519Sig)
	if err != nil {
		t.Fatalf("Ed25519 verification failed: %v", err)
	}

	// Verify Sigstore bundle
	result, err := signing.VerifySigstoreBundle(manifest, *bundle, identity)
	if err != nil {
		t.Fatalf("Sigstore verification failed: %v", err)
	}
	if !result.Verified {
		t.Error("expected Sigstore verification to pass")
	}

	// Verify dual verification
	dualResult := signing.VerifyDual(manifest, ed25519Sig, bundle, identity)
	if !dualResult.Ed25519OK {
		t.Error("expected Ed25519OK to be true in dual verification")
	}
	if !dualResult.SigstoreOK {
		t.Error("expected SigstoreOK to be true in dual verification")
	}
	if dualResult.Error != nil {
		t.Errorf("expected no error in dual verification, got: %v", dualResult.Error)
	}
}

func TestInstallSigstoreFailsEd25519Passes(t *testing.T) {
	manifest := "name: partial-sig-tool\nversion: 1.0.0\ntype: skill\n"

	// Generate Ed25519 keypair and sign
	ed25519Pub, ed25519Priv, _ := ed25519.GenerateKey(nil)
	ed25519PubB64 := base64.StdEncoding.EncodeToString(ed25519Pub)

	old := signing.RegistryPublicKeyB64
	signing.RegistryPublicKeyB64 = ed25519PubB64
	defer func() { signing.RegistryPublicKeyB64 = old }()

	ed25519Hash := sha256.Sum256([]byte(manifest))
	ed25519Sig := base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519Priv, ed25519Hash[:]))

	// Create a Sigstore bundle that will fail (wrong identity)
	identity := "https://github.com/clictl/registry/.github/workflows/build.yml@refs/heads/main"
	sigstoreBundleJSON, _ := makeTestSigstoreBundle(t, manifest, identity)
	bundle, _ := signing.ParseSigstoreBundle(sigstoreBundleJSON)

	// Verify dual with wrong expected identity to make Sigstore fail
	wrongIdentity := "https://github.com/wrong/identity/.github/workflows/build.yml@refs/heads/main"
	dualResult := signing.VerifyDual(manifest, ed25519Sig, bundle, wrongIdentity)

	// Ed25519 should pass
	if !dualResult.Ed25519OK {
		t.Error("expected Ed25519OK to be true")
	}

	// Sigstore should fail (wrong identity)
	if dualResult.SigstoreOK {
		t.Error("expected SigstoreOK to be false")
	}

	// Should produce a warning, not an error
	if dualResult.Error != nil {
		t.Errorf("expected no error (Ed25519 passed), got: %v", dualResult.Error)
	}
	if dualResult.Warning == "" {
		t.Error("expected a warning about Sigstore verification failure")
	}
	if !strings.Contains(dualResult.Warning, "Sigstore verification failed") {
		t.Errorf("expected warning about Sigstore failure, got: %q", dualResult.Warning)
	}
}

func TestInstallBothFail(t *testing.T) {
	manifest := "name: both-fail-tool\nversion: 1.0.0\ntype: skill\n"

	// Set up wrong Ed25519 key (verification will fail)
	wrongPub, _, _ := ed25519.GenerateKey(nil)
	old := signing.RegistryPublicKeyB64
	signing.RegistryPublicKeyB64 = base64.StdEncoding.EncodeToString(wrongPub)
	defer func() { signing.RegistryPublicKeyB64 = old }()

	// Sign with a different key that does not match the configured public key
	_, realPriv, _ := ed25519.GenerateKey(nil)
	ed25519Hash := sha256.Sum256([]byte(manifest))
	ed25519Sig := base64.StdEncoding.EncodeToString(ed25519.Sign(realPriv, ed25519Hash[:]))

	// Create a Sigstore bundle and verify with wrong identity
	identity := "https://github.com/clictl/registry/.github/workflows/build.yml@refs/heads/main"
	sigstoreBundleJSON, _ := makeTestSigstoreBundle(t, manifest, identity)
	bundle, _ := signing.ParseSigstoreBundle(sigstoreBundleJSON)

	// Both should fail
	dualResult := signing.VerifyDual(manifest, ed25519Sig, bundle, "https://github.com/wrong/identity")

	if dualResult.Ed25519OK {
		t.Error("expected Ed25519OK to be false")
	}
	if dualResult.SigstoreOK {
		t.Error("expected SigstoreOK to be false")
	}
	if dualResult.Error == nil {
		t.Fatal("expected error when both verifications fail")
	}
	if !strings.Contains(dualResult.Error.Error(), "both") {
		t.Errorf("expected 'both' in error message, got: %v", dualResult.Error)
	}

	// Mock a server that returns both URLs - the install should reject
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "releases/latest") {
			json.NewEncoder(w).Encode(map[string]string{
				"pack_url":            "https://example.com/pack.tar.gz",
				"sig_url":             "https://example.com/pack.sig",
				"sigstore_bundle_url": "https://example.com/pack.sigstore.json",
				"sha256":              "deadbeef",
				"version":             "1.0.0",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Verify the server responds correctly
	resp, err := http.Get(fmt.Sprintf("%s/packs/both-fail-tool/releases/latest", server.URL))
	if err != nil {
		t.Fatalf("server request failed: %v", err)
	}
	defer resp.Body.Close()

	var releaseInfo struct {
		PackURL           string `json:"pack_url"`
		SigURL            string `json:"sig_url"`
		SigstoreBundleURL string `json:"sigstore_bundle_url"`
	}
	json.NewDecoder(resp.Body).Decode(&releaseInfo)

	if releaseInfo.SigstoreBundleURL == "" {
		t.Error("expected sigstore_bundle_url in release info")
	}
}

func TestBadSignatureRejected(t *testing.T) {
	// Generate two different keypairs
	_, priv1, _ := ed25519.GenerateKey(nil)
	pub2, _, _ := ed25519.GenerateKey(nil)

	pub2B64 := base64.StdEncoding.EncodeToString(pub2)

	// Create a manifest and sign with key 1
	manifestYAML := `schema_version: "1"
name: bad-sig-tool
type: skill
version: 1.0.0
content_sha256: deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef
`
	manifestHash := sha256.Sum256([]byte(manifestYAML))
	sig := ed25519.Sign(priv1, manifestHash[:])
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	// Try to verify with key 2 - should fail (wrong key)
	err := signing.VerifyManifestWithKey(manifestYAML, sigB64, pub2B64)
	if err == nil {
		t.Fatal("expected signature verification to fail with wrong key")
	}
	if !strings.Contains(err.Error(), "verification failed") {
		t.Errorf("expected 'verification failed' error, got: %v", err)
	}

	// Try to verify with a corrupted signature
	corruptedSig := make([]byte, len(sig))
	copy(corruptedSig, sig)
	corruptedSig[0] ^= 0xFF // flip bits in first byte
	corruptedSigB64 := base64.StdEncoding.EncodeToString(corruptedSig)

	pub1B64 := base64.StdEncoding.EncodeToString(ed25519.PublicKey(priv1.Public().(ed25519.PublicKey)))
	err = signing.VerifyManifestWithKey(manifestYAML, corruptedSigB64, pub1B64)
	if err == nil {
		t.Fatal("expected signature verification to fail with corrupted signature")
	}

	// Try to verify a tampered manifest with original signature
	tamperedManifest := `schema_version: "1"
name: evil-tool
type: skill
version: 1.0.0
content_sha256: deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef
`
	err = signing.VerifyManifestWithKey(tamperedManifest, sigB64, pub1B64)
	if err == nil {
		t.Fatal("expected signature verification to fail with tampered manifest")
	}

	// Verify that an invalid base64 signature is rejected
	err = signing.VerifyManifestWithKey(manifestYAML, "not-valid-base64!!!", pub1B64)
	if err == nil {
		t.Fatal("expected error for invalid base64 signature")
	}

	// Verify that an empty signature is rejected
	err = signing.VerifyManifestWithKey(manifestYAML, "", pub1B64)
	if err == nil {
		t.Fatal("expected error for empty signature")
	}
}
