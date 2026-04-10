// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package sandbox

import (
	"os"
)

// essentialVars are always passed to child processes regardless of spec declarations.
// These are required for basic process operation across platforms.
var essentialVars = []string{
	"PATH",
	"HOME",
	"TMPDIR",
	"LANG",
	"USER",
	"SHELL",
	"TERM",
	// Windows essentials
	"SYSTEMROOT",
	"WINDIR",
	"COMSPEC",
	"TEMP",
	"TMP",
	"USERPROFILE",
	"APPDATA",
	"LOCALAPPDATA",
	"PROGRAMFILES",
	"PROGRAMFILES(X86)",
	"COMMONPROGRAMFILES",
	// Node.js runtime
	"NODE_PATH",
}

// BuildEnv constructs a clean environment for a sandboxed MCP server subprocess.
//
// Instead of inheriting all parent env vars (which exposes AWS_SECRET_ACCESS_KEY,
// GITHUB_TOKEN, etc. to potentially compromised packages), this builds an allowlist:
//
//  1. Essential system vars (PATH, HOME, etc.)
//  2. Spec sandbox.env.allow declarations
//  3. Spec auth.env vars
//  4. Spec server.env literal values
//  5. CLICTL_SANDBOX=1 marker
func BuildEnv(policy *Policy) []string {
	env := make([]string, 0, 32)

	// 1. Essential system vars
	for _, key := range essentialVars {
		if val, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+val)
		}
	}

	// 2. Declared sandbox.env.allow
	if policy.Spec.Sandbox != nil && policy.Spec.Sandbox.Env != nil {
		for _, key := range policy.Spec.Sandbox.Env.Allow {
			if val, ok := os.LookupEnv(key); ok {
				env = append(env, key+"="+val)
			}
		}
	}

	// 3. Auth env vars
	if policy.Spec.Auth != nil {
		for _, key := range policy.Spec.Auth.Env {
			if val, ok := os.LookupEnv(key); ok {
				env = append(env, key+"="+val)
			}
		}
	}

	// 4. Server env (literal values from spec YAML)
	if policy.Spec.Server != nil {
		for k, v := range policy.Spec.Server.Env {
			env = append(env, k+"="+v)
		}
	}

	// 5. Sandbox marker
	env = append(env, "CLICTL_SANDBOX=1")

	return env
}
