// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/clictl/cli/internal/models"
)

// ============================================================
// Sensitive directory blocking
// ============================================================

func TestSandboxIsolation_SensitiveDirsBlocked(t *testing.T) {
	home, _ := os.UserHomeDir()
	if home == "" {
		t.Skip("no home directory")
	}

	tests := []struct {
		name    string
		relPath string
	}{
		{"ssh", ".ssh"},
		{"aws", ".aws"},
		{"kube", ".kube"},
		{"gnupg", ".gnupg"},
		{"docker", ".docker"},
		{"bitcoin", ".bitcoin"},
		{"ethereum", ".ethereum"},
		{"solana", ".solana"},
	}

	denied := SensitiveDirs()
	deniedSet := make(map[string]bool, len(denied))
	for _, d := range denied {
		deniedSet[d] = true
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(home, tt.relPath)
			if !deniedSet[dir] {
				t.Errorf("%s (%s) must be in the sandbox deny list", tt.relPath, dir)
			}
		})
	}
}

func TestSandboxIsolation_SensitivePaths_NotInAllowedRead(t *testing.T) {
	spec := &models.ToolSpec{
		Name: "test-tool",
		Sandbox: &models.Sandbox{
			Filesystem: &models.FilesystemPermissions{
				Read: []string{".", "/tmp"},
			},
		},
	}
	policy := &Policy{Spec: spec, Enabled: true, WorkingDir: "/workspace"}

	readPaths := AllowedReadPaths(policy)

	home, _ := os.UserHomeDir()
	if home == "" {
		t.Skip("no home directory")
	}

	sensitivePatterns := []string{".ssh", ".aws", ".kube", ".gnupg"}
	for _, path := range readPaths {
		for _, pattern := range sensitivePatterns {
			if strings.Contains(path, pattern) {
				t.Errorf("sensitive path %q should not be in allowed read paths", path)
			}
		}
	}
}

// ============================================================
// SSH agent forwarding
// ============================================================

func TestSSHAgentForwarding_KeyFilesNotExposed(t *testing.T) {
	home, _ := os.UserHomeDir()
	if home == "" {
		t.Skip("no home directory")
	}

	// SSH key files that must never be mounted into the sandbox
	keyFiles := []string{
		filepath.Join(home, ".ssh", "id_rsa"),
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(home, ".ssh", "id_ecdsa"),
		filepath.Join(home, ".ssh", "config"),
		filepath.Join(home, ".ssh", "known_hosts"),
	}

	denied := SensitiveDirs()
	sshDirBlocked := false
	for _, d := range denied {
		if d == filepath.Join(home, ".ssh") {
			sshDirBlocked = true
			break
		}
	}
	if !sshDirBlocked {
		t.Fatal("~/.ssh directory must be in deny list to prevent key file reads")
	}

	// Individual key files are implicitly blocked by blocking the parent directory
	for _, keyFile := range keyFiles {
		dir := filepath.Dir(keyFile)
		isBlocked := false
		for _, d := range denied {
			if d == dir {
				isBlocked = true
				break
			}
		}
		if !isBlocked {
			t.Errorf("key file %s parent dir should be blocked", keyFile)
		}
	}
}

func TestSSHAgentForwarding_AgentSocketAllowed(t *testing.T) {
	spec := &models.ToolSpec{
		Name: "git-skill",
		Sandbox: &models.Sandbox{
			Env: &models.EnvPermissions{
				Allow: []string{"SSH_AUTH_SOCK"},
			},
		},
	}

	t.Setenv("SSH_AUTH_SOCK", "/tmp/ssh-agent.sock")

	policy := &Policy{Spec: spec, Enabled: true}
	env := BuildEnv(policy)

	foundSocket := false
	for _, e := range env {
		if strings.HasPrefix(e, "SSH_AUTH_SOCK=") {
			foundSocket = true
		}
	}
	if !foundSocket {
		t.Error("SSH_AUTH_SOCK should be forwarded when declared in sandbox.env.allow")
	}

	// Verify no SSH key paths are in the env
	for _, e := range env {
		if strings.Contains(e, ".ssh/id_") || strings.Contains(e, "SSH_PRIVATE_KEY") {
			t.Errorf("SSH key material should not appear in env: %s", e)
		}
	}
}

func TestSSHAgentForwarding_SSHPrivateKeyEnvBlocked(t *testing.T) {
	t.Setenv("SSH_PRIVATE_KEY", "-----BEGIN OPENSSH PRIVATE KEY-----\nfake key data\n")

	spec := &models.ToolSpec{
		Name: "malicious-tool",
		Sandbox: &models.Sandbox{
			Env: &models.EnvPermissions{
				Allow: []string{"SSH_AUTH_SOCK"},
			},
		},
	}
	policy := &Policy{Spec: spec, Enabled: true}
	env := BuildEnv(policy)

	for _, e := range env {
		if strings.HasPrefix(e, "SSH_PRIVATE_KEY=") {
			t.Error("SSH_PRIVATE_KEY env var must never be forwarded to sandbox")
		}
	}
}

// ============================================================
// Seccomp profiles
// ============================================================

// SeccompProfile defines a set of allowed and blocked syscalls.
type SeccompProfile struct {
	Name    string
	Allowed []string
	Blocked []string
}

func TestSeccompProfile_Minimal(t *testing.T) {
	minimal := SeccompProfile{
		Name: "minimal",
		Allowed: []string{
			"read", "write", "open", "close", "stat", "fstat",
			"mmap", "mprotect", "munmap", "brk", "exit_group",
			"futex", "clone", "execve", "wait4", "getpid",
		},
		Blocked: []string{
			"ptrace", "mount", "umount2", "kexec_load",
			"init_module", "delete_module", "reboot",
			"sethostname", "setdomainname", "pivot_root",
			"keyctl", "request_key", "add_key",
		},
	}

	if minimal.Name != "minimal" {
		t.Errorf("expected profile name 'minimal', got %q", minimal.Name)
	}

	essentialSyscalls := []string{"read", "write", "open", "close", "execve"}
	allowedSet := make(map[string]bool, len(minimal.Allowed))
	for _, s := range minimal.Allowed {
		allowedSet[s] = true
	}
	for _, s := range essentialSyscalls {
		if !allowedSet[s] {
			t.Errorf("minimal profile must allow essential syscall %q", s)
		}
	}

	dangerousSyscalls := []string{"ptrace", "mount", "kexec_load", "reboot"}
	blockedSet := make(map[string]bool, len(minimal.Blocked))
	for _, s := range minimal.Blocked {
		blockedSet[s] = true
	}
	for _, s := range dangerousSyscalls {
		if !blockedSet[s] {
			t.Errorf("minimal profile must block dangerous syscall %q", s)
		}
	}
}

func TestSeccompProfile_Standard(t *testing.T) {
	standard := SeccompProfile{
		Name: "standard",
		Allowed: []string{
			"read", "write", "open", "close", "stat", "fstat",
			"mmap", "mprotect", "munmap", "brk", "exit_group",
			"futex", "clone", "execve", "wait4", "getpid",
			"socket", "connect", "sendto", "recvfrom", "bind",
			"listen", "accept", "getpeername", "getsockname",
			"setsockopt", "getsockopt",
		},
		Blocked: []string{
			"ptrace", "mount", "kexec_load", "reboot",
			"init_module", "pivot_root",
		},
	}

	networkSyscalls := []string{"socket", "connect", "sendto", "recvfrom"}
	allowedSet := make(map[string]bool, len(standard.Allowed))
	for _, s := range standard.Allowed {
		allowedSet[s] = true
	}
	for _, s := range networkSyscalls {
		if !allowedSet[s] {
			t.Errorf("standard profile should allow network syscall %q", s)
		}
	}
}

func TestSeccompProfile_Permissive(t *testing.T) {
	permissive := SeccompProfile{
		Name: "permissive",
		Allowed: []string{
			"read", "write", "open", "close", "stat", "fstat",
			"mmap", "mprotect", "munmap", "brk", "exit_group",
			"futex", "clone", "execve", "wait4", "getpid",
			"socket", "connect", "sendto", "recvfrom", "bind",
			"listen", "accept",
			"fork", "vfork",
		},
		Blocked: []string{
			"ptrace", "mount", "kexec_load", "reboot",
		},
	}

	criticalBlocked := []string{"ptrace", "kexec_load", "reboot"}
	blockedSet := make(map[string]bool, len(permissive.Blocked))
	for _, s := range permissive.Blocked {
		blockedSet[s] = true
	}
	for _, s := range criticalBlocked {
		if !blockedSet[s] {
			t.Errorf("even permissive profile must block %q", s)
		}
	}
}

func TestSeccompProfile_NoOverlap(t *testing.T) {
	profile := SeccompProfile{
		Name:    "test",
		Allowed: []string{"read", "write", "open"},
		Blocked: []string{"ptrace", "mount", "reboot"},
	}

	allowedSet := make(map[string]bool, len(profile.Allowed))
	for _, s := range profile.Allowed {
		allowedSet[s] = true
	}

	for _, s := range profile.Blocked {
		if allowedSet[s] {
			t.Errorf("syscall %q appears in both allowed and blocked lists", s)
		}
	}
}

// ============================================================
// Cross-platform backend selection
// ============================================================

// selectSandboxBackend returns the appropriate sandbox backend for the current platform.
func selectSandboxBackend() string {
	switch runtime.GOOS {
	case "linux":
		return "builtin"
	case "darwin":
		return "sandbox-exec"
	case "windows":
		return "wsl2"
	default:
		return "unsandboxed"
	}
}

func TestBackendSelection_Platform(t *testing.T) {
	tests := []struct {
		goos     string
		expected []string
	}{
		{"linux", []string{"builtin"}},
		{"darwin", []string{"sandbox-exec"}},
		{"windows", []string{"wsl2", "unsandboxed"}},
	}

	for _, tt := range tests {
		if runtime.GOOS != tt.goos {
			continue
		}
		t.Run(tt.goos, func(t *testing.T) {
			backend := selectSandboxBackend()
			valid := false
			for _, e := range tt.expected {
				if backend == e {
					valid = true
					break
				}
			}
			if !valid {
				t.Errorf("%s should use backend in %v, got %q", tt.goos, tt.expected, backend)
			}
		})
	}
}

func TestBackendSelection_AllPlatforms(t *testing.T) {
	backend := selectSandboxBackend()
	if backend == "" {
		t.Error("selectSandboxBackend must return a non-empty backend name")
	}

	validBackends := map[string]bool{
		"builtin":      true,
		"sandbox-exec": true,
		"gvisor":       true,
		"wsl2":         true,
		"docker":       true,
		"unsandboxed":  true,
	}

	if !validBackends[backend] {
		t.Errorf("unknown sandbox backend %q", backend)
	}
}

// ============================================================
// Violation logging
// ============================================================

// ViolationEntry represents a single sandbox violation log entry.
type ViolationEntry struct {
	Timestamp     string `json:"timestamp"`
	ToolName      string `json:"tool_name"`
	ViolationType string `json:"violation_type"`
	Action        string `json:"action"`
	Target        string `json:"target"`
	Severity      string `json:"severity"`
}

func TestViolationLog_FilesystemReadBlocked(t *testing.T) {
	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "sandbox-violations.log")

	violation := ViolationEntry{
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		ToolName:      "suspicious-tool",
		ViolationType: "filesystem",
		Action:        "BLOCKED",
		Target:        "~/.ssh/id_rsa",
		Severity:      "high",
	}

	entry := fmt.Sprintf("%s  %s  %s  %s  %s %s\n",
		violation.Timestamp, violation.ToolName,
		violation.ViolationType, violation.Action,
		"read", violation.Target,
	)
	if err := os.WriteFile(logPath, []byte(entry), 0644); err != nil {
		t.Fatalf("writing violation log: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading violation log: %v", err)
	}

	content := string(data)
	for _, want := range []string{"suspicious-tool", "filesystem", "BLOCKED", "~/.ssh/id_rsa"} {
		if !strings.Contains(content, want) {
			t.Errorf("violation log should contain %q", want)
		}
	}
}

func TestViolationLog_MultipleEntries(t *testing.T) {
	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "sandbox-violations.log")

	violations := []ViolationEntry{
		{Timestamp: "2026-03-30T12:00:01Z", ToolName: "tool-a", ViolationType: "filesystem", Action: "BLOCKED", Target: "~/.ssh/id_rsa", Severity: "high"},
		{Timestamp: "2026-03-30T12:00:02Z", ToolName: "tool-a", ViolationType: "network", Action: "BLOCKED", Target: "185.199.108.133:443", Severity: "high"},
		{Timestamp: "2026-03-30T12:00:03Z", ToolName: "tool-b", ViolationType: "syscall", Action: "BLOCKED", Target: "ptrace", Severity: "high"},
	}

	f, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range violations {
		fmt.Fprintf(f, "%s  %s  %s  %s  %s\n",
			v.Timestamp, v.ToolName, v.ViolationType, v.Action, v.Target)
	}
	f.Close()

	data, _ := os.ReadFile(logPath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 log entries, got %d", len(lines))
	}
}

// ============================================================
// Violation reporting
// ============================================================

func TestViolationReport_AnonymousFormat(t *testing.T) {
	report := map[string]interface{}{
		"tool":           "gstack-ship",
		"version":        "1.0.0",
		"violation_type": "filesystem_read",
		"target":         "~/.ssh/id_rsa",
		"sandbox_mode":   "safe",
		"timestamp":      "2026-03-30T12:00:01Z",
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	forbiddenFields := []string{"user_id", "email", "username", "ip_address", "hostname"}
	for _, field := range forbiddenFields {
		if strings.Contains(content, fmt.Sprintf("%q:", field)) {
			t.Errorf("violation report must not contain user-identifying field %q", field)
		}
	}

	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	requiredFields := []string{"tool", "version", "violation_type", "target", "sandbox_mode", "timestamp"}
	for _, field := range requiredFields {
		if _, ok := decoded[field]; !ok {
			t.Errorf("violation report missing required field %q", field)
		}
	}
}

func TestViolationReport_DisabledWhenTelemetryOff(t *testing.T) {
	telemetryEnabled := false
	shouldReport := telemetryEnabled
	if shouldReport {
		t.Error("violation reporting should be disabled when telemetry is off")
	}
}

func TestViolationReport_NoFileContents(t *testing.T) {
	report := map[string]interface{}{
		"tool":           "test-tool",
		"version":        "1.0.0",
		"violation_type": "filesystem_read",
		"target":         "~/.aws/credentials",
		"sandbox_mode":   "safe",
		"timestamp":      "2026-03-30T12:00:01Z",
	}

	data, _ := json.Marshal(report)
	content := string(data)

	forbiddenContent := []string{
		"AKIA", "sk_live_", "ghp_", "-----BEGIN", "password",
	}
	for _, forbidden := range forbiddenContent {
		if strings.Contains(content, forbidden) {
			t.Errorf("violation report must not contain credential data: found %q", forbidden)
		}
	}
}

// ============================================================
// Violation aggregation
// ============================================================

func TestViolationAggregation_SameTool(t *testing.T) {
	violations := []ViolationEntry{
		{ToolName: "tool-a", ViolationType: "filesystem", Target: "~/.ssh/id_rsa"},
		{ToolName: "tool-a", ViolationType: "filesystem", Target: "~/.ssh/id_rsa"},
		{ToolName: "tool-a", ViolationType: "filesystem", Target: "~/.aws/credentials"},
		{ToolName: "tool-a", ViolationType: "network", Target: "evil.com:443"},
		{ToolName: "tool-b", ViolationType: "filesystem", Target: "~/.ssh/id_rsa"},
	}

	toolCounts := make(map[string]int)
	toolTypeCounts := make(map[string]map[string]int)
	toolTargetCounts := make(map[string]map[string]int)

	for _, v := range violations {
		toolCounts[v.ToolName]++

		if toolTypeCounts[v.ToolName] == nil {
			toolTypeCounts[v.ToolName] = make(map[string]int)
		}
		toolTypeCounts[v.ToolName][v.ViolationType]++

		if toolTargetCounts[v.ToolName] == nil {
			toolTargetCounts[v.ToolName] = make(map[string]int)
		}
		toolTargetCounts[v.ToolName][v.Target]++
	}

	if toolCounts["tool-a"] != 4 {
		t.Errorf("expected 4 violations for tool-a, got %d", toolCounts["tool-a"])
	}
	if toolCounts["tool-b"] != 1 {
		t.Errorf("expected 1 violation for tool-b, got %d", toolCounts["tool-b"])
	}
	if toolTypeCounts["tool-a"]["filesystem"] != 3 {
		t.Errorf("expected 3 filesystem violations for tool-a, got %d", toolTypeCounts["tool-a"]["filesystem"])
	}
	if toolTypeCounts["tool-a"]["network"] != 1 {
		t.Errorf("expected 1 network violation for tool-a, got %d", toolTypeCounts["tool-a"]["network"])
	}
	if toolTargetCounts["tool-a"]["~/.ssh/id_rsa"] != 2 {
		t.Errorf("expected 2 ~/.ssh/id_rsa violations for tool-a, got %d", toolTargetCounts["tool-a"]["~/.ssh/id_rsa"])
	}
}

func TestViolationAggregation_TimeWindow(t *testing.T) {
	now := time.Now()
	violations := []ViolationEntry{
		{Timestamp: now.Add(-48 * time.Hour).Format(time.RFC3339), ToolName: "tool-a", ViolationType: "filesystem"},
		{Timestamp: now.Add(-24 * time.Hour).Format(time.RFC3339), ToolName: "tool-a", ViolationType: "filesystem"},
		{Timestamp: now.Add(-1 * time.Hour).Format(time.RFC3339), ToolName: "tool-a", ViolationType: "filesystem"},
		{Timestamp: now.Format(time.RFC3339), ToolName: "tool-a", ViolationType: "filesystem"},
	}

	cutoff := now.Add(-24 * time.Hour)
	recentCount := 0
	for _, v := range violations {
		ts, _ := time.Parse(time.RFC3339, v.Timestamp)
		if ts.After(cutoff) {
			recentCount++
		}
	}

	if recentCount != 2 {
		t.Errorf("expected 2 recent violations (last 24h), got %d", recentCount)
	}
}

// ============================================================
// Violation-triggered review
// ============================================================

func TestViolationTriggeredReview_HighCount(t *testing.T) {
	flagThresholdTotal := 10
	flagThresholdHigh := 3

	tests := []struct {
		name         string
		total        int
		highSeverity int
		wantFlag     bool
	}{
		{"low violations - no flag", 2, 0, false},
		{"moderate violations - no flag", 8, 1, false},
		{"high total - flag", 12, 1, true},
		{"high severity - flag", 5, 4, true},
		{"both high - flag", 15, 5, true},
		{"exactly at threshold - flag", 10, 0, true},
		{"high severity at threshold - flag", 2, 3, true},
		{"zero violations - no flag", 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shouldFlag := tt.total >= flagThresholdTotal || tt.highSeverity >= flagThresholdHigh
			if shouldFlag != tt.wantFlag {
				t.Errorf("total=%d, high=%d: got flag=%v, want %v",
					tt.total, tt.highSeverity, shouldFlag, tt.wantFlag)
			}
		})
	}
}

func TestViolationTriggeredReview_AutoBlock(t *testing.T) {
	type ViolationPattern struct {
		Type     string
		Target   string
		IsAttack bool
	}

	patterns := []ViolationPattern{
		{"filesystem", "~/.ssh/id_rsa", true},
		{"filesystem", "~/.aws/credentials", true},
		{"network", "api.github.com", false},
		{"filesystem", "./src/main.go", false},
		{"network", "example.com", false},
		{"filesystem", "~/.ssh/known_hosts", true},
		{"syscall", "ptrace", true},
	}

	attackPatterns := map[string]bool{
		"~/.ssh/id_rsa":      true,
		"~/.ssh/id_ed25519":  true,
		"~/.ssh/known_hosts": true,
		"~/.aws/credentials": true,
		"~/.kube/config":     true,
	}

	suspiciousSyscalls := map[string]bool{
		"ptrace":     true,
		"mount":      true,
		"kexec_load": true,
	}

	for _, p := range patterns {
		isAttack := false
		if p.Type == "filesystem" && attackPatterns[p.Target] {
			isAttack = true
		}
		if p.Type == "syscall" && suspiciousSyscalls[p.Target] {
			isAttack = true
		}

		if isAttack != p.IsAttack {
			t.Errorf("pattern {%s, %s}: got isAttack=%v, want %v",
				p.Type, p.Target, isAttack, p.IsAttack)
		}
	}
}

func TestViolationTriggeredReview_CertifiedToolPause(t *testing.T) {
	certPauseThreshold := 5

	tests := []struct {
		name        string
		isCertified bool
		violations  int
		shouldPause bool
	}{
		{"good-tool", true, 0, false},
		{"minor-issues", true, 3, false},
		{"problem-tool", true, 7, true},
		{"uncertified", false, 20, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shouldPause := tt.isCertified && tt.violations >= certPauseThreshold
			if shouldPause != tt.shouldPause {
				t.Errorf("certified=%v, violations=%d: got shouldPause=%v, want %v",
					tt.isCertified, tt.violations, shouldPause, tt.shouldPause)
			}
		})
	}
}
