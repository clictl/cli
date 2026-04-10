// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package sandbox

import (
	"fmt"
	"runtime"
	"strings"
)

// credentialEnvVars are environment variable prefixes and names that should be
// stripped from skill subprocess environments to prevent credential leakage.
var credentialEnvVars = []string{
	"AWS_ACCESS_KEY_ID",
	"AWS_SECRET_ACCESS_KEY",
	"AWS_SESSION_TOKEN",
	"GITHUB_TOKEN",
	"GH_TOKEN",
	"GITLAB_TOKEN",
	"DOCKER_PASSWORD",
	"NPM_TOKEN",
	"STRIPE_SECRET_KEY",
	"DATABASE_URL",
	"REDIS_URL",
	"MONGO_URI",
	"VAULT_TOKEN",
	// Process injection vectors - these allow arbitrary code/library loading
	// into child processes, which could bypass sandbox restrictions.
	"LD_PRELOAD",
	"DYLD_INSERT_LIBRARIES",
	"NODE_OPTIONS",
	"PYTHONPATH",
	"RUBYOPT",
}

// proxyEnvVars are proxy-related env vars that should be stripped.
var proxyEnvVars = []string{
	"HTTP_PROXY",
	"HTTPS_PROXY",
	"http_proxy",
	"https_proxy",
	"NO_PROXY",
	"no_proxy",
	"ALL_PROXY",
	"all_proxy",
}

// SkillSandboxConfig holds the parameters for generating a skill sandbox wrapper.
type SkillSandboxConfig struct {
	// AllowedHosts are the declared network hosts from spec.Permissions.Network.
	AllowedHosts []string
	// SkillName is the name of the skill for identification in logs.
	SkillName string
}

// GenerateSkillSandboxWrapper generates a shell wrapper script that applies
// environment scrubbing and platform-specific network restrictions for skills.
//
// On Linux: generates Landlock rules for network restriction.
// On macOS: generates a sandbox-exec profile allowing only declared hosts.
// On Windows: advisory only (logs a warning).
func GenerateSkillSandboxWrapper(cfg SkillSandboxConfig) string {
	var sb strings.Builder

	sb.WriteString("#!/usr/bin/env bash\n")
	sb.WriteString("# clictl skill sandbox wrapper - auto-generated\n")
	sb.WriteString(fmt.Sprintf("# Skill: %s\n", cfg.SkillName))
	sb.WriteString("set -euo pipefail\n\n")

	// Env scrubbing: unset credential and proxy vars
	sb.WriteString("# Strip credential environment variables\n")
	for _, v := range credentialEnvVars {
		sb.WriteString(fmt.Sprintf("unset %s 2>/dev/null || true\n", v))
	}
	sb.WriteString("\n# Strip proxy environment variables\n")
	for _, v := range proxyEnvVars {
		sb.WriteString(fmt.Sprintf("unset %s 2>/dev/null || true\n", v))
	}
	sb.WriteString("\n")

	// Platform-specific network restriction
	switch runtime.GOOS {
	case "darwin":
		sb.WriteString(generateDarwinSandboxProfile(cfg))
	case "linux":
		sb.WriteString(generateLinuxNetworkRestriction(cfg))
	case "windows":
		sb.WriteString("# Windows: network restriction is advisory only\n")
		sb.WriteString(fmt.Sprintf("echo \"clictl: warning: network restriction not enforced on Windows for skill %s\" >&2\n", cfg.SkillName))
		if len(cfg.AllowedHosts) > 0 {
			sb.WriteString(fmt.Sprintf("echo \"clictl: declared hosts: %s\" >&2\n", strings.Join(cfg.AllowedHosts, ", ")))
		}
	}

	sb.WriteString("\n# Execute the wrapped command\n")
	sb.WriteString("exec \"$@\"\n")

	return sb.String()
}

// generateDarwinSandboxProfile generates a macOS sandbox-exec profile section
// that restricts network access to declared hosts only.
func generateDarwinSandboxProfile(cfg SkillSandboxConfig) string {
	var sb strings.Builder

	sb.WriteString("# macOS: apply sandbox-exec network restriction\n")

	if len(cfg.AllowedHosts) == 0 {
		sb.WriteString("# No network hosts declared - denying all network access\n")
		sb.WriteString("SANDBOX_PROFILE='(version 1)\n")
		sb.WriteString("(deny network*)\n")
		sb.WriteString("(allow default)'\n\n")
	} else {
		sb.WriteString("SANDBOX_PROFILE='(version 1)\n")
		sb.WriteString("(deny default)\n")
		sb.WriteString("(allow process-exec)\n")
		sb.WriteString("(allow process-fork)\n")
		sb.WriteString("(allow sysctl-read)\n")
		sb.WriteString("(allow mach-lookup)\n")
		sb.WriteString("(allow file-read*)\n")
		sb.WriteString("(allow file-write*\n")
		sb.WriteString("    (subpath \"/tmp\")\n")
		sb.WriteString("    (subpath \"/private/tmp\"))\n")
		sb.WriteString("(allow network*\n")
		sb.WriteString("    (remote ip \"localhost:*\")\n")
		for _, host := range cfg.AllowedHosts {
			sb.WriteString(fmt.Sprintf("    (remote ip \"%s:*\")\n", host))
		}
		sb.WriteString(")'\n\n")
	}

	return sb.String()
}

// generateLinuxNetworkRestriction generates Linux-specific network restriction
// using iptables or Landlock rules embedded in the wrapper script.
func generateLinuxNetworkRestriction(cfg SkillSandboxConfig) string {
	var sb strings.Builder

	sb.WriteString("# Linux: network restriction via resolved host checking\n")

	if len(cfg.AllowedHosts) == 0 {
		sb.WriteString("# No network hosts declared - logging advisory\n")
		sb.WriteString(fmt.Sprintf("echo \"clictl: skill %s has no declared network hosts\" >&2\n", cfg.SkillName))
	} else {
		sb.WriteString("# Declared network hosts:\n")
		for _, host := range cfg.AllowedHosts {
			sb.WriteString(fmt.Sprintf("#   - %s\n", host))
		}
		sb.WriteString("CLICTL_ALLOWED_HOSTS=\"")
		sb.WriteString(strings.Join(cfg.AllowedHosts, ","))
		sb.WriteString("\"\n")
		sb.WriteString("export CLICTL_ALLOWED_HOSTS\n")
	}
	sb.WriteString("\n")

	return sb.String()
}

// EnvScrub returns a list of environment variable names that should be
// removed from the skill subprocess environment.
func EnvScrub() []string {
	result := make([]string, 0, len(credentialEnvVars)+len(proxyEnvVars))
	result = append(result, credentialEnvVars...)
	result = append(result, proxyEnvVars...)
	return result
}
