// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.

// Package vault provides encrypted secret storage. Secrets are stored in vault.enc
// files (AES-256-GCM) with keys in vault.key files (32 random bytes, 0600 perms).
// Resolution order: project vault > user vault > workspace vault > raw env var.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"crypto/pbkdf2"
)

// validVaultKey restricts vault key names to safe identifiers.
// Names must start with a letter or underscore, followed by letters, digits,
// underscores, dots, hyphens, or colons. Colons are allowed for internal
// namespaced keys such as workspace cache entries (ws:workspace:KEY).
// Path separators (/ and \) and other special characters are rejected.
var validVaultKey = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.:-]*$`)

const (
	// pbkdf2Iterations is the number of PBKDF2 iterations for password-derived keys.
	pbkdf2Iterations = 600_000
	// pbkdf2SaltSize is the size of the PBKDF2 salt in bytes.
	pbkdf2SaltSize = 16
	// keySize is the AES-256 key size in bytes.
	keySize = 32
	// nonceSize is the AES-GCM nonce size in bytes.
	nonceSize = 12
)

// VaultEntryMeta contains metadata about a stored secret (never the value).
type VaultEntryMeta struct {
	Name  string    `json:"name"`
	SetAt time.Time `json:"set_at"`
}

// vaultEntry is the internal representation of a stored secret.
type vaultEntry struct {
	Value string    `json:"value"`
	SetAt time.Time `json:"set_at"`
}

// vaultData is the plaintext structure stored inside the encrypted vault file.
type vaultData struct {
	Entries map[string]vaultEntry `json:"entries"`
}

// Vault provides encrypted secret storage backed by a key file and data file.
type Vault struct {
	keyPath  string
	dataPath string
}

// NewVault creates a vault pointing to the user-level vault files in configDir.
// Typically configDir is ~/.clictl.
func NewVault(configDir string) *Vault {
	return &Vault{
		keyPath:  filepath.Join(configDir, "vault.key"),
		dataPath: filepath.Join(configDir, "vault.enc"),
	}
}

// NewFileVault creates a vault using file-based key storage.
// Equivalent to NewVault; retained for API compatibility with tests.
func NewFileVault(configDir string) *Vault {
	return NewVault(configDir)
}

// NewProjectVault creates a vault pointing to project-level vault files
// in projectDir/.clictl/.
func NewProjectVault(projectDir string) *Vault {
	dir := filepath.Join(projectDir, ".clictl")
	return &Vault{
		keyPath:  filepath.Join(dir, "vault.key"),
		dataPath: filepath.Join(dir, "vault.enc"),
	}
}

// KeyPath returns the path to the vault key file.
func (v *Vault) KeyPath() string {
	return v.keyPath
}

// DataPath returns the path to the encrypted vault data file.
func (v *Vault) DataPath() string {
	return v.dataPath
}

// SetNoKeyring is a no-op retained for API compatibility.
func (v *Vault) SetNoKeyring(_ bool) {}

// InitKey generates a 32-byte random encryption key and writes it to the key file.
// If a key already exists, this is a no-op.
func (v *Vault) InitKey() error {
	if _, err := os.Stat(v.keyPath); err == nil {
		return nil
	}

	key := make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return fmt.Errorf("generating random key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(v.keyPath), 0o700); err != nil {
		return fmt.Errorf("creating vault directory: %w", err)
	}

	return atomicWrite(v.keyPath, key, 0o600)
}

// InitKeyForce generates a new key regardless of whether one exists.
// This also removes the existing vault data file since the old key is lost.
func (v *Vault) InitKeyForce() error {
	_ = os.Remove(v.dataPath)
	_ = os.Remove(v.keyPath)

	key := make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return fmt.Errorf("generating random key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(v.keyPath), 0o700); err != nil {
		return fmt.Errorf("creating vault directory: %w", err)
	}

	return atomicWrite(v.keyPath, key, 0o600)
}

// InitKeyFromPassword derives an encryption key from a password using PBKDF2-SHA256.
// A random salt is prepended to the key file so the key can be re-derived.
// Format of key file: [16-byte salt][32-byte derived key].
func (v *Vault) InitKeyFromPassword(password string) error {
	salt := make([]byte, pbkdf2SaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return fmt.Errorf("generating salt: %w", err)
	}

	derived, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, keySize)
	if err != nil {
		return fmt.Errorf("deriving key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(v.keyPath), 0o700); err != nil {
		return fmt.Errorf("creating vault directory: %w", err)
	}

	// Remove existing vault data since key is changing
	_ = os.Remove(v.dataPath)

	data := make([]byte, pbkdf2SaltSize+keySize)
	copy(data[:pbkdf2SaltSize], salt)
	copy(data[pbkdf2SaltSize:], derived)
	return atomicWrite(v.keyPath, data, 0o600)
}

// Set encrypts and stores a secret in the vault.
func (v *Vault) Set(name, value string) error {
	if !validVaultKey.MatchString(name) {
		return fmt.Errorf("invalid secret name %q: must match [a-zA-Z_][a-zA-Z0-9_.:-]*", name)
	}
	if strings.ContainsRune(value, 0) {
		return fmt.Errorf("secret value cannot contain null bytes")
	}
	return v.withLock(func() error {
		data, err := v.readData()
		if err != nil {
			return err
		}

		data.Entries[name] = vaultEntry{
			Value: value,
			SetAt: time.Now().UTC(),
		}

		return v.writeData(data)
	})
}

// Get decrypts and returns a secret from the vault.
func (v *Vault) Get(name string) (string, error) {
	if !validVaultKey.MatchString(name) {
		return "", fmt.Errorf("invalid secret name %q: must match [a-zA-Z_][a-zA-Z0-9_.:-]*", name)
	}
	var result string
	err := v.withLock(func() error {
		data, err := v.readData()
		if err != nil {
			return err
		}

		entry, ok := data.Entries[name]
		if !ok {
			return fmt.Errorf("secret %q not found in vault", name)
		}

		result = entry.Value
		return nil
	})
	return result, err
}

// List returns metadata for all secrets in the vault (never values).
func (v *Vault) List() ([]VaultEntryMeta, error) {
	var metas []VaultEntryMeta
	err := v.withLock(func() error {
		data, err := v.readData()
		if err != nil {
			return err
		}

		for name, entry := range data.Entries {
			metas = append(metas, VaultEntryMeta{
				Name:  name,
				SetAt: entry.SetAt,
			})
		}
		return nil
	})
	return metas, err
}

// Delete removes a secret from the vault.
func (v *Vault) Delete(name string) error {
	return v.withLock(func() error {
		data, err := v.readData()
		if err != nil {
			return err
		}

		if _, ok := data.Entries[name]; !ok {
			return fmt.Errorf("secret %q not found in vault", name)
		}

		delete(data.Entries, name)
		return v.writeData(data)
	})
}

// Exists returns true if the named secret is stored in the vault.
func (v *Vault) Exists(name string) bool {
	exists := false
	_ = v.withLock(func() error {
		data, err := v.readData()
		if err != nil {
			return err
		}
		_, exists = data.Entries[name]
		return nil
	})
	return exists
}

// HasKey returns true if the vault key file exists.
func (v *Vault) HasKey() bool {
	_, err := os.Stat(v.keyPath)
	return err == nil
}

// CheckPermissions verifies that vault files have restrictive permissions (0600).
// Returns an error describing any permission issues found.
func (v *Vault) CheckPermissions() error {
	var issues []string

	if info, err := os.Stat(v.keyPath); err == nil {
		perm := info.Mode().Perm()
		if perm != 0o600 {
			issues = append(issues, fmt.Sprintf("%s has permissions %04o (expected 0600)", v.keyPath, perm))
		}
	}

	if info, err := os.Stat(v.dataPath); err == nil {
		perm := info.Mode().Perm()
		if perm != 0o600 {
			issues = append(issues, fmt.Sprintf("%s has permissions %04o (expected 0600)", v.dataPath, perm))
		}
	}

	if len(issues) > 0 {
		return fmt.Errorf("vault permission issues:\n  %s", strings.Join(issues, "\n  "))
	}
	return nil
}

// readKey loads the encryption key from the key file.
// The key file may contain just 32 bytes (random key) or 48 bytes (16-byte salt + 32-byte key).
func (v *Vault) readKey() ([]byte, error) {
	data, err := os.ReadFile(v.keyPath)
	if err != nil {
		return nil, fmt.Errorf("reading vault key: %w", err)
	}

	switch len(data) {
	case keySize:
		return data, nil
	case pbkdf2SaltSize + keySize:
		// Password-derived: the actual key is after the salt
		return data[pbkdf2SaltSize:], nil
	default:
		return nil, fmt.Errorf("invalid vault key file size: %d bytes", len(data))
	}
}

// readData decrypts and parses the vault data file.
// Returns empty data if the file does not exist.
func (v *Vault) readData() (*vaultData, error) {
	data := &vaultData{Entries: make(map[string]vaultEntry)}

	ciphertext, err := os.ReadFile(v.dataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return data, nil
		}
		return nil, fmt.Errorf("reading vault data: %w", err)
	}

	if len(ciphertext) == 0 {
		return data, nil
	}

	key, err := v.readKey()
	if err != nil {
		return nil, err
	}

	plaintext, err := decrypt(key, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypting vault data: %w", err)
	}

	if err := json.Unmarshal(plaintext, data); err != nil {
		return nil, fmt.Errorf("parsing vault data: %w", err)
	}

	return data, nil
}

// writeData encrypts and writes the vault data to the data file.
func (v *Vault) writeData(data *vaultData) error {
	plaintext, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshaling vault data: %w", err)
	}

	key, err := v.readKey()
	if err != nil {
		return err
	}

	ciphertext, err := encrypt(key, plaintext)
	if err != nil {
		return fmt.Errorf("encrypting vault data: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(v.dataPath), 0o700); err != nil {
		return fmt.Errorf("creating vault directory: %w", err)
	}

	return atomicWrite(v.dataPath, ciphertext, 0o600)
}

// withLock acquires an exclusive file lock on the data file for the duration of fn.
// The platform-specific locking is implemented in lock_unix.go and lock_windows.go.
func (v *Vault) withLock(fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(v.dataPath), 0o700); err != nil {
		return fmt.Errorf("creating vault directory: %w", err)
	}

	lockPath := v.dataPath + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("creating vault lock file: %w", err)
	}

	if err := lockFileExclusive(lockFile); err != nil {
		lockFile.Close()
		return fmt.Errorf("acquiring vault lock: %w", err)
	}
	defer func() {
		unlockFile(lockFile)
		lockFile.Close()
		// Do not remove the lock file - removing it while another process
		// holds an flock on the same inode causes new lockers to create a
		// different file, breaking mutual exclusion.
	}()

	return fn()
}

// encrypt uses AES-256-GCM with a random nonce.
// Output format: [12-byte nonce][ciphertext+tag].
func encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// decrypt uses AES-256-GCM. Input format: [12-byte nonce][ciphertext+tag].
func decrypt(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}

	if len(data) < gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce := data[:gcm.NonceSize()]
	ciphertext := data[gcm.NonceSize():]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypting: %w", err)
	}

	return plaintext, nil
}

// atomicWrite writes data to a temp file and renames it into place.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".vault-tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing temp file: %w", err)
	}

	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("setting permissions: %w", err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming temp file: %w", err)
	}

	return nil
}

// EnsureGitignore checks if a .gitignore exists in the given directory and
// adds vault file entries if they are missing. If no .gitignore exists,
// it prints a warning to stderr.
func EnsureGitignore(projectDir string) {
	gitignorePath := filepath.Join(projectDir, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Warning: No .gitignore found. Make sure .clictl/vault.key is not committed.\n")
			return
		}
		return
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	hasKey := false
	hasEnc := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == ".clictl/vault.key" {
			hasKey = true
		}
		if trimmed == ".clictl/vault.enc" {
			hasEnc = true
		}
	}

	if hasKey && hasEnc {
		return
	}

	var toAdd []string
	if !hasKey {
		toAdd = append(toAdd, ".clictl/vault.key")
	}
	if !hasEnc {
		toAdd = append(toAdd, ".clictl/vault.enc")
	}

	// Ensure trailing newline before appending
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	content += strings.Join(toAdd, "\n") + "\n"
	_ = os.WriteFile(gitignorePath, []byte(content), 0o644)
}
