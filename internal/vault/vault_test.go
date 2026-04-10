// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package vault

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSetGetRoundtrip(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)

	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	if err := v.Set("API_KEY", "sk_live_abc123"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := v.Get("API_KEY")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "sk_live_abc123" {
		t.Errorf("Get = %q, want %q", got, "sk_live_abc123")
	}
}

func TestSetOverwrite(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	if err := v.Set("KEY", "old"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := v.Set("KEY", "new"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := v.Get("KEY")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "new" {
		t.Errorf("Get = %q, want %q", got, "new")
	}
}

func TestListReturnsNamesNotValues(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	if err := v.Set("SECRET_A", "value_a"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := v.Set("SECRET_B", "value_b"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	metas, err := v.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(metas) != 2 {
		t.Fatalf("List returned %d entries, want 2", len(metas))
	}

	names := make([]string, len(metas))
	for i, m := range metas {
		names[i] = m.Name
	}
	sort.Strings(names)

	if names[0] != "SECRET_A" || names[1] != "SECRET_B" {
		t.Errorf("List names = %v, want [SECRET_A, SECRET_B]", names)
	}

	// Verify SetAt is populated
	for _, m := range metas {
		if m.SetAt.IsZero() {
			t.Errorf("SetAt is zero for %s", m.Name)
		}
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	if err := v.Set("TO_DELETE", "secret"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if err := v.Delete("TO_DELETE"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if v.Exists("TO_DELETE") {
		t.Error("Exists returned true after Delete")
	}

	_, err := v.Get("TO_DELETE")
	if err == nil {
		t.Error("Get succeeded after Delete, expected error")
	}
}

func TestDeleteNotFound(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	err := v.Delete("NONEXISTENT")
	if err == nil {
		t.Error("Delete of non-existent key should return error")
	}
}

func TestInitKeyIdempotent(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)

	if err := v.InitKey(); err != nil {
		t.Fatalf("first InitKey: %v", err)
	}

	// Read the key
	key1, err := os.ReadFile(v.KeyPath())
	if err != nil {
		t.Fatalf("reading key: %v", err)
	}

	// Second init should not change the key
	if err := v.InitKey(); err != nil {
		t.Fatalf("second InitKey: %v", err)
	}

	key2, err := os.ReadFile(v.KeyPath())
	if err != nil {
		t.Fatalf("reading key: %v", err)
	}

	if string(key1) != string(key2) {
		t.Error("InitKey changed the key on second call, should be idempotent")
	}
}

func TestInitKeyForce(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)

	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}
	if err := v.Set("KEY", "value"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	key1, _ := os.ReadFile(v.KeyPath())

	if err := v.InitKeyForce(); err != nil {
		t.Fatalf("InitKeyForce: %v", err)
	}

	key2, _ := os.ReadFile(v.KeyPath())
	if string(key1) == string(key2) {
		t.Error("InitKeyForce did not generate a new key")
	}

	// Data should be gone
	if _, err := os.Stat(v.DataPath()); !os.IsNotExist(err) {
		t.Error("InitKeyForce did not remove the data file")
	}
}

func TestInitKeyFromPassword(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)

	if err := v.InitKeyFromPassword("my-strong-password"); err != nil {
		t.Fatalf("InitKeyFromPassword: %v", err)
	}

	// Key file should be salt + key = 48 bytes
	data, err := os.ReadFile(v.KeyPath())
	if err != nil {
		t.Fatalf("reading key file: %v", err)
	}
	if len(data) != pbkdf2SaltSize+keySize {
		t.Errorf("key file size = %d, want %d", len(data), pbkdf2SaltSize+keySize)
	}

	// Should be able to store and retrieve
	if err := v.Set("PW_KEY", "pw_value"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := v.Get("PW_KEY")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "pw_value" {
		t.Errorf("Get = %q, want %q", got, "pw_value")
	}
}

func TestFilePermissions(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}
	if err := v.Set("KEY", "val"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Permissions should be 0600
	if err := v.CheckPermissions(); err != nil {
		t.Errorf("CheckPermissions failed on fresh vault: %v", err)
	}

	// Make key world-readable and check
	os.Chmod(v.KeyPath(), 0o644)
	if err := v.CheckPermissions(); err == nil {
		t.Error("CheckPermissions should fail with 0644 permissions")
	}
}

func TestCheckPermissionsNoFiles(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)

	// No files exist, should pass
	if err := v.CheckPermissions(); err != nil {
		t.Errorf("CheckPermissions should pass when no files exist: %v", err)
	}
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	if v.Exists("NOPE") {
		t.Error("Exists returned true for non-existent key")
	}

	if err := v.Set("YES", "val"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if !v.Exists("YES") {
		t.Error("Exists returned false for existing key")
	}
}

func TestHasKey(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)

	if v.HasKey() {
		t.Error("HasKey returned true before InitKey")
	}

	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	if !v.HasKey() {
		t.Error("HasKey returned false after InitKey")
	}
}

func TestNewProjectVault(t *testing.T) {
	dir := t.TempDir()
	v := NewProjectVault(dir)

	expected := filepath.Join(dir, ".clictl", "vault.key")
	if v.KeyPath() != expected {
		t.Errorf("KeyPath = %q, want %q", v.KeyPath(), expected)
	}
}

func TestResolveEnvBasic(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}
	if err := v.Set("DB_PASS", "secret123"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	env := map[string]string{
		"DB_HOST": "localhost",
		"DB_PASS": "vault://DB_PASS",
		"DB_NAME": "mydb",
	}

	resolved := ResolveEnv(env, nil, v)

	if resolved["DB_HOST"] != "localhost" {
		t.Errorf("DB_HOST = %q, want %q", resolved["DB_HOST"], "localhost")
	}
	if resolved["DB_PASS"] != "secret123" {
		t.Errorf("DB_PASS = %q, want %q", resolved["DB_PASS"], "secret123")
	}
	if resolved["DB_NAME"] != "mydb" {
		t.Errorf("DB_NAME = %q, want %q", resolved["DB_NAME"], "mydb")
	}
}

func TestResolveEnvProjectShadowsUser(t *testing.T) {
	projectDir := t.TempDir()
	userDir := t.TempDir()

	projectVault := NewFileVault(projectDir)
	userVault := NewFileVault(userDir)

	if err := projectVault.InitKey(); err != nil {
		t.Fatalf("project InitKey: %v", err)
	}
	if err := userVault.InitKey(); err != nil {
		t.Fatalf("user InitKey: %v", err)
	}

	// Both have the same key but different values
	if err := projectVault.Set("API_KEY", "project_key"); err != nil {
		t.Fatalf("project Set: %v", err)
	}
	if err := userVault.Set("API_KEY", "user_key"); err != nil {
		t.Fatalf("user Set: %v", err)
	}
	// Only user vault has this one
	if err := userVault.Set("OTHER_KEY", "user_other"); err != nil {
		t.Fatalf("user Set: %v", err)
	}

	env := map[string]string{
		"API_KEY":   "vault://API_KEY",
		"OTHER_KEY": "vault://OTHER_KEY",
	}

	resolved := ResolveEnv(env, projectVault, userVault)

	// Project vault should shadow user vault
	if resolved["API_KEY"] != "project_key" {
		t.Errorf("API_KEY = %q, want %q (project should shadow user)", resolved["API_KEY"], "project_key")
	}
	// Fallback to user vault
	if resolved["OTHER_KEY"] != "user_other" {
		t.Errorf("OTHER_KEY = %q, want %q", resolved["OTHER_KEY"], "user_other")
	}
}

func TestResolveEnvMissingKey(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	env := map[string]string{
		"MISSING": "vault://NONEXISTENT",
	}

	resolved := ResolveEnv(env, nil, v)

	// Should preserve original reference
	if resolved["MISSING"] != "vault://NONEXISTENT" {
		t.Errorf("MISSING = %q, want original reference preserved", resolved["MISSING"])
	}
}

func TestResolveEnvNilVaults(t *testing.T) {
	env := map[string]string{
		"KEY": "vault://SOMETHING",
	}

	resolved := ResolveEnv(env, nil, nil)

	// Should preserve original
	if resolved["KEY"] != "vault://SOMETHING" {
		t.Errorf("KEY = %q, want original", resolved["KEY"])
	}
}

func TestConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	var wg sync.WaitGroup
	errors := make(chan error, 20)

	// 10 goroutines writing
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "KEY_" + strings.Repeat("A", n)
			if err := v.Set(key, "value"); err != nil {
				errors <- err
			}
		}(i)
	}

	// 10 goroutines reading
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = v.List()
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent access error: %v", err)
	}
}

func TestAtomicWriteNoCorruption(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	// Write many entries sequentially, verify we can read after each
	for i := 0; i < 50; i++ {
		key := "KEY_" + string(rune('A'+i%26))
		if err := v.Set(key, "value"); err != nil {
			t.Fatalf("Set(%s): %v", key, err)
		}
	}

	// Should be able to list all entries
	metas, err := v.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) == 0 {
		t.Error("List returned 0 entries after writing 50")
	}
}

func TestIsVaultRef(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"vault://API_KEY", true},
		{"vault://", true},
		{"VAULT://KEY", false},
		{"sk_live_abc123", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := IsVaultRef(tt.value); got != tt.want {
			t.Errorf("IsVaultRef(%q) = %v, want %v", tt.value, got, tt.want)
		}
	}
}

func TestVaultRefName(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{"vault://API_KEY", "API_KEY"},
		{"vault://", ""},
		{"not-a-ref", ""},
	}

	for _, tt := range tests {
		if got := VaultRefName(tt.value); got != tt.want {
			t.Errorf("VaultRefName(%q) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	key := make([]byte, keySize)
	for i := range key {
		key[i] = byte(i)
	}

	plaintext := []byte("hello world, this is a secret")
	ciphertext, err := encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Ciphertext should be different from plaintext
	if string(ciphertext) == string(plaintext) {
		t.Error("ciphertext equals plaintext")
	}

	got, err := decrypt(key, ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if string(got) != string(plaintext) {
		t.Errorf("decrypt = %q, want %q", string(got), string(plaintext))
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1 := make([]byte, keySize)
	key2 := make([]byte, keySize)
	key2[0] = 0xFF

	ciphertext, err := encrypt(key1, []byte("secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	_, err = decrypt(key2, ciphertext)
	if err == nil {
		t.Error("decrypt with wrong key should fail")
	}
}

func TestEnsureGitignore(t *testing.T) {
	dir := t.TempDir()

	// Create a .gitignore without vault entries
	gitignore := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gitignore, []byte("node_modules/\n.env\n"), 0o644); err != nil {
		t.Fatalf("writing .gitignore: %v", err)
	}

	EnsureGitignore(dir)

	data, err := os.ReadFile(gitignore)
	if err != nil {
		t.Fatalf("reading .gitignore: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, ".clictl/vault.key") {
		t.Error(".gitignore missing .clictl/vault.key")
	}
	if !strings.Contains(content, ".clictl/vault.enc") {
		t.Error(".gitignore missing .clictl/vault.enc")
	}
}

func TestEnsureGitignoreIdempotent(t *testing.T) {
	dir := t.TempDir()
	gitignore := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(gitignore, []byte(".clictl/vault.key\n.clictl/vault.enc\n"), 0o644); err != nil {
		t.Fatalf("writing .gitignore: %v", err)
	}

	EnsureGitignore(dir)

	data, _ := os.ReadFile(gitignore)
	count := strings.Count(string(data), ".clictl/vault.key")
	if count != 1 {
		t.Errorf(".clictl/vault.key appears %d times, want 1", count)
	}
}

func TestSetValidName(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	// Valid names should work
	validNames := []string{"API_KEY", "my_secret", "DB_HOST_1", "_private", "key.name", "my-key"}
	for _, name := range validNames {
		if err := v.Set(name, "value"); err != nil {
			t.Errorf("Set(%q) should succeed, got: %v", name, err)
		}
	}
}

func TestSetInvalidName(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	invalidNames := []string{
		"../../../etc/passwd",
		"secret/name",
		"",
		"123start",
		"name with spaces",
		"key=value",
		"$ENV_VAR",
		"name\x00null",
	}
	for _, name := range invalidNames {
		if err := v.Set(name, "value"); err == nil {
			t.Errorf("Set(%q) should return error for invalid name", name)
		}
	}
}

func TestSetNullByteInValue(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	err := v.Set("VALID_KEY", "value\x00with\x00nulls")
	if err == nil {
		t.Error("Set with null bytes in value should return error")
	}
	if err != nil && !strings.Contains(err.Error(), "null bytes") {
		t.Errorf("error should mention null bytes, got: %v", err)
	}
}

func TestGetInvalidName(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	_, err := v.Get("../traversal")
	if err == nil {
		t.Error("Get with path traversal name should return error")
	}
}

func TestResolveEnvValidName(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}
	if err := v.Set("GOOD_KEY", "resolved_value"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	env := map[string]string{
		"MY_VAR": "vault://GOOD_KEY",
	}
	resolved := ResolveEnv(env, nil, v)
	if resolved["MY_VAR"] != "resolved_value" {
		t.Errorf("expected resolved_value, got %q", resolved["MY_VAR"])
	}
}

func TestResolveEnvInvalidNamePathTraversal(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	env := map[string]string{
		"EVIL": "vault://../../etc/passwd",
	}
	resolved := ResolveEnv(env, nil, v)

	// Should keep original value, not attempt lookup
	if resolved["EVIL"] != "vault://../../etc/passwd" {
		t.Errorf("expected original reference preserved for invalid name, got %q", resolved["EVIL"])
	}
}

func TestResolveEnvInvalidNameSpecialChars(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	env := map[string]string{
		"BAD1": "vault://key with spaces",
		"BAD2": "vault://$VAR",
		"BAD3": "vault://123numeric",
	}
	resolved := ResolveEnv(env, nil, v)

	for key, val := range env {
		if resolved[key] != val {
			t.Errorf("expected original reference preserved for %s, got %q", key, resolved[key])
		}
	}
}

func TestLockOrderCorrectness(t *testing.T) {
	// Verify the lock/unlock/close/remove sequence works correctly
	// by doing concurrent operations that exercise withLock
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "LOCK_TEST_" + strconv.Itoa(n)
			if err := v.Set(key, "value"); err != nil {
				errs <- err
			}
			if _, err := v.Get(key); err != nil {
				errs <- err
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent lock order test error: %v", err)
	}
}

func TestEnsureGitignoreNoFile(t *testing.T) {
	dir := t.TempDir()
	// No .gitignore exists - should not crash, just warn
	EnsureGitignore(dir)
	// No file should be created
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); !os.IsNotExist(err) {
		t.Error(".gitignore should not be created when it does not exist")
	}
}

func TestMultipleSecrets(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	secrets := map[string]string{
		"STRIPE_KEY":    "sk_live_abc",
		"GITHUB_TOKEN":  "ghp_def",
		"DB_PASSWORD":   "super_secret",
		"AWS_ACCESS_ID": "AKIAIOSFODNN",
	}

	for k, val := range secrets {
		if err := v.Set(k, val); err != nil {
			t.Fatalf("Set(%s): %v", k, err)
		}
	}

	for k, want := range secrets {
		got, err := v.Get(k)
		if err != nil {
			t.Fatalf("Get(%s): %v", k, err)
		}
		if got != want {
			t.Errorf("Get(%s) = %q, want %q", k, got, want)
		}
	}

	metas, err := v.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != len(secrets) {
		t.Errorf("List returned %d entries, want %d", len(metas), len(secrets))
	}
}

func TestGetNotFound(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	_, err := v.Get("NONEXISTENT")
	if err == nil {
		t.Error("Get of non-existent key should return error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestEmptyVaultList(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	metas, err := v.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 0 {
		t.Errorf("List returned %d entries on empty vault, want 0", len(metas))
	}
}

// ---------------------------------------------------------------------------
// Workspace vault resolver tests (merged from workspace_test.go)
// ---------------------------------------------------------------------------

func TestWorkspaceResolve_CacheHit(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	// Pre-populate cache with a fresh entry
	cacheKey := "ws:test-workspace:MY_SECRET"
	entry := workspaceCacheEntry{
		Value:     "cached-value",
		ETag:      `"abc123"`,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	data, _ := json.Marshal(entry)
	if err := v.Set(cacheKey, string(data)); err != nil {
		t.Fatalf("Set cache: %v", err)
	}

	// Create resolver with a server that should NOT be called
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("API should not be called on cache hit")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	resolver := NewWorkspaceVaultResolver(server.URL, "test-token", "test-workspace", v)
	got := resolver.Resolve("MY_SECRET")

	if got != "cached-value" {
		t.Errorf("Resolve = %q, want %q", got, "cached-value")
	}
}

func TestWorkspaceResolve_CacheMiss(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	// No cache entry - API should be called
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Bearer auth, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("ETag", `"new-etag"`)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"value": "api-value"})
	}))
	defer server.Close()

	resolver := NewWorkspaceVaultResolver(server.URL, "test-token", "my-ws", v)
	got := resolver.Resolve("API_KEY")

	if got != "api-value" {
		t.Errorf("Resolve = %q, want %q", got, "api-value")
	}

	// Verify it was cached
	cacheKey := "ws:my-ws:API_KEY"
	raw, err := v.Get(cacheKey)
	if err != nil {
		t.Fatalf("cache not populated: %v", err)
	}
	var cached workspaceCacheEntry
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		t.Fatalf("parsing cached entry: %v", err)
	}
	if cached.Value != "api-value" {
		t.Errorf("cached value = %q, want %q", cached.Value, "api-value")
	}
	if cached.ETag != `"new-etag"` {
		t.Errorf("cached etag = %q, want %q", cached.ETag, `"new-etag"`)
	}
}

func TestWorkspaceResolve_304ExtendsCache(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	// Pre-populate cache with an expired entry
	cacheKey := "ws:my-ws:SECRET"
	entry := workspaceCacheEntry{
		Value:     "stale-value",
		ETag:      `"old-etag"`,
		ExpiresAt: time.Now().Add(-1 * time.Minute), // expired
	}
	data, _ := json.Marshal(entry)
	if err := v.Set(cacheKey, string(data)); err != nil {
		t.Fatalf("Set cache: %v", err)
	}

	// Server returns 304 Not Modified
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != `"old-etag"` {
			t.Errorf("expected If-None-Match header with old etag, got %q", r.Header.Get("If-None-Match"))
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	resolver := NewWorkspaceVaultResolver(server.URL, "test-token", "my-ws", v)
	got := resolver.Resolve("SECRET")

	if got != "stale-value" {
		t.Errorf("Resolve = %q, want %q (should return cached value on 304)", got, "stale-value")
	}

	// Verify cache was extended
	raw, _ := v.Get(cacheKey)
	var updated workspaceCacheEntry
	json.Unmarshal([]byte(raw), &updated)
	if time.Now().After(updated.ExpiresAt) {
		t.Error("cache TTL was not extended after 304")
	}
}

func TestWorkspaceResolve_ErrorWithStaleCache(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	// Pre-populate cache with an expired entry
	cacheKey := "ws:my-ws:SECRET"
	entry := workspaceCacheEntry{
		Value:     "stale-value",
		ETag:      `"old-etag"`,
		ExpiresAt: time.Now().Add(-1 * time.Minute), // expired
	}
	data, _ := json.Marshal(entry)
	if err := v.Set(cacheKey, string(data)); err != nil {
		t.Fatalf("Set cache: %v", err)
	}

	// Server returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	resolver := NewWorkspaceVaultResolver(server.URL, "test-token", "my-ws", v)
	got := resolver.Resolve("SECRET")

	if got != "stale-value" {
		t.Errorf("Resolve = %q, want %q (should use stale cache on error)", got, "stale-value")
	}
}

func TestWorkspaceResolve_ErrorNoCache(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	// No cache, server errors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	resolver := NewWorkspaceVaultResolver(server.URL, "test-token", "my-ws", v)
	got := resolver.Resolve("MISSING")

	if got != "" {
		t.Errorf("Resolve = %q, want empty string on error with no cache", got)
	}
}

func TestResolveEnvWithWorkspace(t *testing.T) {
	projectDir := t.TempDir()
	userDir := t.TempDir()

	projectVault := NewFileVault(projectDir)
	userVault := NewFileVault(userDir)

	if err := projectVault.InitKey(); err != nil {
		t.Fatalf("project InitKey: %v", err)
	}
	if err := userVault.InitKey(); err != nil {
		t.Fatalf("user InitKey: %v", err)
	}

	// Project vault has KEY_A
	if err := projectVault.Set("KEY_A", "project-a"); err != nil {
		t.Fatalf("project Set: %v", err)
	}
	// User vault has KEY_B
	if err := userVault.Set("KEY_B", "user-b"); err != nil {
		t.Fatalf("user Set: %v", err)
	}

	// Workspace resolver has KEY_C
	wsDir := t.TempDir()
	wsVault := NewFileVault(wsDir)
	if err := wsVault.InitKey(); err != nil {
		t.Fatalf("ws InitKey: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"ws-etag"`)
		json.NewEncoder(w).Encode(map[string]string{"value": "workspace-c"})
	}))
	defer server.Close()

	wsResolver := NewWorkspaceVaultResolver(server.URL, "token", "slug", wsVault)

	env := map[string]string{
		"KEY_A": "vault://KEY_A",
		"KEY_B": "vault://KEY_B",
		"KEY_C": "vault://KEY_C",
		"KEY_D": "plain-value",
	}

	resolved := ResolveEnv(env, projectVault, userVault, WithWorkspaceResolver(wsResolver))

	// Project vault wins for KEY_A
	if resolved["KEY_A"] != "project-a" {
		t.Errorf("KEY_A = %q, want %q", resolved["KEY_A"], "project-a")
	}
	// User vault wins for KEY_B
	if resolved["KEY_B"] != "user-b" {
		t.Errorf("KEY_B = %q, want %q", resolved["KEY_B"], "user-b")
	}
	// Workspace resolver handles KEY_C
	if resolved["KEY_C"] != "workspace-c" {
		t.Errorf("KEY_C = %q, want %q", resolved["KEY_C"], "workspace-c")
	}
	// Plain value passes through
	if resolved["KEY_D"] != "plain-value" {
		t.Errorf("KEY_D = %q, want %q", resolved["KEY_D"], "plain-value")
	}
}

func TestWorkspaceResolve_StaleCacheBeyondMaxTTL(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	// Pre-populate cache with an entry that expired more than maxStaleTTL ago
	cacheKey := "ws:my-ws:OLD_SECRET"
	entry := workspaceCacheEntry{
		Value:     "very-stale-value",
		ETag:      `"old-etag"`,
		ExpiresAt: time.Now().Add(-2 * time.Hour), // expired 2 hours ago, beyond maxStaleTTL (1 hour)
	}
	data, _ := json.Marshal(entry)
	if err := v.Set(cacheKey, string(data)); err != nil {
		t.Fatalf("Set cache: %v", err)
	}

	// Server returns an error so fallback is triggered
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server unavailable"))
	}))
	defer server.Close()

	resolver := NewWorkspaceVaultResolver(server.URL, "test-token", "my-ws", v)
	got := resolver.Resolve("OLD_SECRET")

	if got != "" {
		t.Errorf("Resolve = %q, want empty string for cache beyond maxStaleTTL", got)
	}
}

func TestWorkspaceResolve_StaleCacheWithinMaxTTL(t *testing.T) {
	dir := t.TempDir()
	v := NewFileVault(dir)
	if err := v.InitKey(); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	// Pre-populate cache with an entry that expired less than maxStaleTTL ago
	cacheKey := "ws:my-ws:RECENT_SECRET"
	entry := workspaceCacheEntry{
		Value:     "recently-stale-value",
		ETag:      `"recent-etag"`,
		ExpiresAt: time.Now().Add(-30 * time.Minute), // expired 30 min ago, within maxStaleTTL (1 hour)
	}
	data, _ := json.Marshal(entry)
	if err := v.Set(cacheKey, string(data)); err != nil {
		t.Fatalf("Set cache: %v", err)
	}

	// Server returns an error so fallback is triggered
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server unavailable"))
	}))
	defer server.Close()

	resolver := NewWorkspaceVaultResolver(server.URL, "test-token", "my-ws", v)
	got := resolver.Resolve("RECENT_SECRET")

	if got != "recently-stale-value" {
		t.Errorf("Resolve = %q, want %q (stale cache within maxStaleTTL should still be served)", got, "recently-stale-value")
	}
}

func TestResolveEnvResolutionOrder(t *testing.T) {
	// All three sources have the same key - project should win
	projectDir := t.TempDir()
	userDir := t.TempDir()
	wsDir := t.TempDir()

	projectVault := NewFileVault(projectDir)
	userVault := NewFileVault(userDir)
	wsVault := NewFileVault(wsDir)

	for _, v := range []*Vault{projectVault, userVault, wsVault} {
		if err := v.InitKey(); err != nil {
			t.Fatalf("InitKey: %v", err)
		}
	}

	if err := projectVault.Set("SHARED", "from-project"); err != nil {
		t.Fatalf("project Set: %v", err)
	}
	if err := userVault.Set("SHARED", "from-user"); err != nil {
		t.Fatalf("user Set: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("workspace API should not be called when project vault has the key")
		json.NewEncoder(w).Encode(map[string]string{"value": "from-workspace"})
	}))
	defer server.Close()

	wsResolver := NewWorkspaceVaultResolver(server.URL, "token", "slug", wsVault)

	env := map[string]string{
		"SHARED": "vault://SHARED",
	}

	resolved := ResolveEnv(env, projectVault, userVault, WithWorkspaceResolver(wsResolver))

	if resolved["SHARED"] != "from-project" {
		t.Errorf("SHARED = %q, want %q (project should take precedence)", resolved["SHARED"], "from-project")
	}
}
