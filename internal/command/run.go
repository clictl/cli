// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/enterprise"
	"github.com/clictl/cli/internal/executor"
	"github.com/clictl/cli/internal/logger"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/permissions"
	"github.com/clictl/cli/internal/registry"
	sandboxBackend "github.com/clictl/cli/internal/sandbox"
	"github.com/clictl/cli/internal/telemetry"
	"github.com/clictl/cli/internal/transform"
	"github.com/clictl/cli/internal/vault"
	"github.com/spf13/cobra"
)

var flagQuiet bool
var flagPaginateAll bool

// Run dispatches to the appropriate executor based on the spec's protocol.
// MCP specs (Discover=true) route through DispatchMCP. HTTP specs route through
// the standard executor pipeline. The --json flag skips all transforms for programmatic use.
var execCmd = &cobra.Command{
	Use:     "run <tool> <action> [--param value...]",
	Short:   "Execute a tool action",
	Long:    "Execute a tool action. Any unknown flags are passed as action parameters.",
	Args:    cobra.MinimumNArgs(2),
	// Disable flag parsing so unknown flags are passed through as action params.
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		toolArg, actionName, params, err := parseExecArgs(args)
		if err != nil {
			return err
		}

		// Parse tool@version syntax
		toolName, toolVersion := registry.ParseToolVersion(toolArg)

		ctx := cmd.Context()

		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load config: %v\n", err)
		}

		// Enterprise: require lock file before execution
		ep := enterprise.GetProvider()
		if ep.RequireLockFile() {
			lf, lfErr := LoadLockFile()
			if lfErr != nil || lf == nil || len(lf.Tools) == 0 {
				return fmt.Errorf("workspace requires a lock file. Run: clictl lock")
			}
		}

		cache := registry.NewCache(cfg.CacheDir)

		logger.Debug("resolving spec", logger.F("tool", toolName), logger.F("version", toolVersion))
		spec, err := registry.ResolveSpecVersion(ctx, toolName, toolVersion, cfg, cache, flagNoCache)
		if err != nil {
			// Auto-install if enabled in config
			if cfg != nil && cfg.AutoInstall {
				logger.Info("tool not installed, auto-installing", logger.F("tool", toolName))
				fmt.Fprintf(os.Stderr, "Tool %q not installed. Installing...\n", toolName)

				apiURL := config.ResolveAPIURL(flagAPIURL, cfg)
				installClient := registry.NewClient(apiURL, cache, flagNoCache)
				token := config.ResolveAuthToken(flagAPIKey, cfg)
				if token != "" {
					installClient.AuthToken = token
				}

				installedSpec, _, installErr := installClient.GetSpecYAML(ctx, toolName)
				if installErr == nil {
					target := detectTarget()
					skillPath, skillErr := generateSkillForTarget(installedSpec, target)
					if skillErr == nil {
						fmt.Fprintf(os.Stderr, "Installed %s skill: %s\n", skillTargets[target].label, skillPath)
						_ = addToInstalled(installedSpec.Name)
						spec = installedSpec
					}
				}
			}

			if spec == nil {
				logger.Warn("spec not found", logger.F("tool", toolName), logger.F("error", err.Error()))
				msg := fmt.Sprintf("tool %q not found", toolName)
				if dym := toolSuggestion(toolName, cfg); dym != "" {
					msg += dym
				}
				return fmt.Errorf("%s", msg)
			}
		}

		apiURL := config.ResolveAPIURL(flagAPIURL, cfg)

		// Permission check: if the user is logged in with an active workspace,
		// verify they have access to this tool/action before executing.
		authToken := config.ResolveAuthToken(flagAPIKey, cfg)
		if authToken != "" && cfg.Auth.ActiveWorkspace != "" {
			checker := permissions.NewChecker(apiURL, authToken)
			allowed, canRequest, reason, permErr := checker.Check(ctx, cfg.Auth.ActiveWorkspace, toolName, actionName)
			if permErr != nil {
				logger.Warn("permission check failed, proceeding", logger.F("error", permErr.Error()))
			} else if !allowed {
				msg := fmt.Sprintf("permission denied: you do not have access to %s/%s in workspace %q", toolName, actionName, cfg.Auth.ActiveWorkspace)
				if reason != "" {
					msg += fmt.Sprintf(" (%s)", reason)
				}
				if canRequest {
					msg += fmt.Sprintf("\n\nTo request access, run:\n  clictl request %s --reason \"...\"", toolName)
				}
				return fmt.Errorf("%s", msg)
			}
		}

		// Resolve tool env vars via vault-first resolution
		resolvedEnv := resolveToolEnv(spec)

		// Temporarily inject vault-resolved env vars into the process
		// environment so downstream executors (HTTP, MCP) can read them
		// via os.Getenv during request building. Clean up immediately
		// after execution to avoid leaking secrets to other code paths.
		for k, v := range resolvedEnv {
			os.Setenv(k, v)
		}
		defer func() {
			for k := range resolvedEnv {
				os.Unsetenv(k)
			}
		}()

		// Pre-execution: check if required env vars (auth.env) are set
		checkRequiredEnvVars(spec, resolvedEnv)

		// Version display (only in verbose mode)
		if flagVerbose && spec.Version != "" {
			fmt.Fprintf(os.Stderr, "%s v%s\n", spec.Name, spec.Version)
		}

		regClient := registry.NewClient(apiURL, cache, flagNoCache)
		token := config.ResolveAuthToken(flagAPIKey, cfg)
		if token != "" {
			regClient.AuthToken = token
		}
		go regClient.LogInvocationWithAction(ctx, toolName, actionName, "cli")

		var result []byte

		// Skill protocol: execute skill scripts or show instructions
		if spec.IsSkill() {
			logger.Info("executing skill", logger.F("tool", toolName), logger.F("action", actionName))
			result, err = runSkill(spec, actionName, params)
			telemetry.TrackRun(toolName, actionName, err == nil)
			if err != nil {
				logger.Error("skill execution failed", logger.F("tool", toolName), logger.F("error", err.Error()))
				return fmt.Errorf("execution failed: %w", err)
			}

			fmt.Println(string(result))
			return nil
		}

		// MCP protocol: route through MCP executor (tool name, not action)
		if spec.Discover {
			if !registry.IsMCPToolAllowed(spec, actionName) {
				return fmt.Errorf("tool %q is not allowed by spec %q (check tools.expose/deny config)", actionName, spec.Name)
			}
			logger.Info("executing MCP tool", logger.F("server", toolName), logger.F("tool", actionName))
			// Convert string params to map[string]any for MCP
			mcpArgs := make(map[string]any, len(params))
			for k, v := range params {
				mcpArgs[k] = v
			}

			result, err = executor.DispatchMCP(ctx, spec, actionName, mcpArgs)
			telemetry.TrackRun(toolName, actionName, err == nil)
			if err != nil {
				logger.Error("MCP execution failed", logger.F("server", toolName), logger.F("tool", actionName), logger.F("error", err.Error()))
				return fmt.Errorf("execution failed: %w", err)
			}
		} else {
			// Standard protocol: find action and dispatch
			action, err := registry.FindAction(spec, actionName)
			if err != nil {
				return err
			}

			logger.Info("executing action", logger.F("tool", toolName), logger.F("action", actionName))
			anyParams := make(map[string]any, len(params))
			isJSON := false
			for k, v := range params {
				if k == "__json" {
					isJSON = true
					continue
				}
				anyParams[k] = v
			}
			result, err = executor.DispatchWithFullOptions(ctx, spec, action, anyParams, &executor.DispatchOptions{
				Config:         cfg,
				PaginateAll:    flagPaginateAll,
				SkipTransforms: isJSON,
			})
			telemetry.TrackRun(toolName, actionName, err == nil)
			if err != nil {
				logger.Error("execution failed", logger.F("tool", toolName), logger.F("action", actionName), logger.F("error", err.Error()))
				return fmt.Errorf("execution failed: %w", err)
			}
			logger.Debug("execution complete", logger.F("response_bytes", len(result)))

			// Check for --raw flag (passed as a param since flags are disabled)
			isRaw := false
			for _, p := range os.Args {
				if p == "--raw" {
					isRaw = true
					break
				}
			}

			// Apply transforms unless --raw or --json
			if !isRaw && !isJSON && len(action.Transform) > 0 {
				// Convert typed TransformStep slice to []any for ParseSteps
				rawSteps := make([]any, len(action.Transform))
				for i, step := range action.Transform {
					rawSteps[i] = transformStepToMap(step)
				}
				pipeline, err := transform.ParseSteps(rawSteps)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: invalid transform config: %v\n", err)
				} else if len(pipeline) > 0 {
					var parsed any
					if json.Unmarshal(result, &parsed) == nil {
						transformed, err := pipeline.Apply(parsed)
						if err != nil {
							fmt.Fprintf(os.Stderr, "Warning: transform failed: %v\n", err)
						} else {
							switch v := transformed.(type) {
							case string:
								fmt.Fprintln(os.Stdout, v)
							default:
								enc := json.NewEncoder(os.Stdout)
								enc.SetIndent("", "  ")
								enc.Encode(v)
							}
							return nil
						}
					}
				}
			}
		}

		output := flagOutput
		if output == "" {
			output = "text"
		}

		// Enterprise: audit log
		if ent := enterprise.GetProvider(); ent.AuditLogEnabled() {
			ent.AuditLog("run", map[string]string{
				"tool": toolName, "action": actionName, "status": "ok",
			})
		}

		switch output {
		case "json":
			var parsed interface{}
			if json.Unmarshal(result, &parsed) == nil {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(parsed)
			}
			fmt.Println(string(result))
		default:
			fmt.Println(string(result))
		}

		return nil
	},
}

// parseExecArgs manually parses the exec command arguments since flag parsing is disabled.
// It extracts the tool name, action name, and any --param value pairs.
func parseExecArgs(rawArgs []string) (tool string, action string, params map[string]string, err error) {
	params = make(map[string]string)

	// Phase 1: consume clictl flags and the two positional args (tool, action).
	// Only clictl's own flags are intercepted here. Once both positional args
	// are found, everything remaining is treated as tool params (Phase 2).
	var positional []string
	var i int
	for i = 0; i < len(rawArgs) && len(positional) < 2; i++ {
		arg := rawArgs[i]

		if arg == "--" {
			// Explicit separator: remaining args are positional
			for _, a := range rawArgs[i+1:] {
				positional = append(positional, a)
				if len(positional) == 2 {
					i = i + 1 + (len(positional))
					break
				}
			}
			break
		}

		if consumed := tryConsumeGlobalFlag(rawArgs, &i); consumed {
			continue
		}

		positional = append(positional, arg)
	}

	if len(positional) < 2 {
		return "", "", nil, fmt.Errorf("requires exactly 2 positional arguments: <tool> <action>")
	}

	// Phase 2: everything after tool and action is a tool param.
	// clictl flags are NOT intercepted here, so tool params like --verbose
	// or --output are passed through to the tool.
	remaining := rawArgs[i:]
	for j := 0; j < len(remaining); j++ {
		arg := remaining[j]

		if arg == "--" {
			// After --, pass remaining as-is (future: positional tool args)
			break
		}

		if strings.HasPrefix(arg, "--") {
			key := strings.TrimPrefix(arg, "--")

			// --raw, --json, and --all are clictl flags allowed after the action,
			// since they control output behavior rather than being tool params.
			if key == "raw" {
				params["__raw"] = "true"
				continue
			}
			if key == "json" {
				params["__json"] = "true"
				continue
			}
			if key == "all" {
				flagPaginateAll = true
				continue
			}

			if strings.Contains(key, "=") {
				parts := strings.SplitN(key, "=", 2)
				params[parts[0]] = parts[1]
			} else if j+1 < len(remaining) && !strings.HasPrefix(remaining[j+1], "--") {
				j++
				params[key] = remaining[j]
			} else {
				params[key] = "true"
			}
			continue
		}

		if strings.HasPrefix(arg, "-") && len(arg) == 2 {
			key := string(arg[1])
			if j+1 < len(remaining) && !strings.HasPrefix(remaining[j+1], "-") {
				j++
				params[key] = remaining[j]
			} else {
				params[key] = "true"
			}
			continue
		}
	}

	return positional[0], positional[1], params, nil
}

// tryConsumeGlobalFlag checks if rawArgs[*i] is a clictl global flag and
// consumes it (advancing *i past any value). Returns true if consumed.
func tryConsumeGlobalFlag(rawArgs []string, i *int) bool {
	arg := rawArgs[*i]

	if strings.HasPrefix(arg, "--") {
		key := strings.TrimPrefix(arg, "--")
		switch key {
		case "no-cache":
			flagNoCache = true
			return true
		case "all":
			flagPaginateAll = true
			return true
		case "output":
			if *i+1 < len(rawArgs) {
				*i++
				flagOutput = rawArgs[*i]
			}
			return true
		case "api-url":
			if *i+1 < len(rawArgs) {
				*i++
				flagAPIURL = rawArgs[*i]
			}
			return true
		case "verbose":
			flagVerbose = true
			logger.Init(true, "debug", "", "")
			return true
		case "quiet":
			flagQuiet = true
			return true
		}
	}

	if strings.HasPrefix(arg, "-") && len(arg) == 2 {
		switch arg {
		case "-o":
			if *i+1 < len(rawArgs) {
				*i++
				flagOutput = rawArgs[*i]
			}
			return true
		case "-v":
			flagVerbose = true
			logger.Init(true, "debug", "", "")
			return true
		case "-q":
			flagQuiet = true
			return true
		}
	}

	return false
}

// resolveToolEnv resolves required env vars for a tool spec using the
// vault-first resolution order: project vault, user vault, .env/OS env.
// Returns a map of env var name to resolved value. Only includes keys
// that were successfully resolved.
func resolveToolEnv(spec *models.ToolSpec) map[string]string {
	if spec == nil {
		return nil
	}

	// Find project vault at git root
	var projectVault *vault.Vault
	if root, err := gitRepoRoot(); err == nil {
		projectVault = vault.NewProjectVault(root)
	}

	// User vault at ~/.clictl
	userVault := vault.NewVault(config.BaseDir())

	return resolveToolEnvWith(spec, projectVault, userVault)
}

// resolveToolEnvWith is the internal implementation of resolveToolEnv that
// accepts explicit vault instances. This makes the function testable without
// relying on the filesystem layout or git repository detection.
func resolveToolEnvWith(spec *models.ToolSpec, projectVault, userVault *vault.Vault) map[string]string {
	if spec == nil {
		return nil
	}

	resolved := make(map[string]string)

	if spec.Auth == nil {
		return resolved
	}

	for _, key := range spec.Auth.Env {
		if key == "" {
			continue
		}

		// 1. Check project vault
		if projectVault != nil && projectVault.HasKey() {
			if v, err := projectVault.Get(key); err == nil && v != "" {
				resolved[key] = v
				continue
			}
		}

		// 2. Check user vault
		if userVault != nil && userVault.HasKey() {
			if v, err := userVault.Get(key); err == nil && v != "" {
				resolved[key] = v
				continue
			}
		}

		// 3. Check .env (already loaded into OS env by dotenv) and OS env
		if v := os.Getenv(key); v != "" {
			resolved[key] = v
			continue
		}

		// Not resolved - will be warned about separately
	}

	return resolved
}

// checkRequiredEnvVars warns if a tool's auth.env fields are not set in the
// environment or vault. This does not block execution, only prints warnings
// to stderr. The resolvedEnv map contains keys already resolved from the vault.
func checkRequiredEnvVars(spec *models.ToolSpec, resolvedEnv map[string]string) {
	if spec == nil {
		return
	}
	if spec.Auth == nil {
		return
	}

	for _, envVar := range spec.Auth.Env {
		if envVar == "" {
			continue
		}

		// Already resolved from vault or env
		if _, ok := resolvedEnv[envVar]; ok {
			continue
		}
		if os.Getenv(envVar) != "" {
			continue
		}

		// Build the warning message
		fmt.Fprintf(os.Stderr, "\n[MISSING_AUTH] Warning: %s requires %s but it is not set.\n", spec.Name, envVar)
		fmt.Fprintf(os.Stderr, "\nTo set it:\n  clictl vault set %s <your-key>\n", envVar)
		fmt.Fprintf(os.Stderr, "\nRun `clictl info %s` for more details.\n\n", spec.Name)
	}
}

// transformStepToMap converts a typed TransformStep to a map[string]any
// suitable for passing to transform.ParseSteps.
func transformStepToMap(step models.TransformStep) map[string]any {
	m := map[string]any{"type": step.Type}
	if step.Extract != "" {
		m["extract"] = step.Extract
	}
	if len(step.Select) > 0 {
		m["select"] = step.Select
	}
	if step.Template != "" {
		m["template"] = step.Template
	}
	if step.MaxItems > 0 {
		m["max_items"] = step.MaxItems
	}
	if step.MaxLength > 0 {
		m["max_length"] = step.MaxLength
	}
	if step.Filter != "" {
		m["filter"] = step.Filter
	}
	if step.On != "" {
		m["on"] = step.On
	}
	if step.Value != "" {
		m["value"] = step.Value
	}
	if len(step.Only) > 0 {
		m["only"] = step.Only
	}
	if len(step.Rename) > 0 {
		m["rename"] = step.Rename
	}
	if step.Flatten {
		m["flatten"] = true
	}
	if step.Unwrap {
		m["unwrap"] = true
	}
	if step.Field != "" {
		m["field"] = step.Field
	}
	if step.Order != "" {
		m["order"] = step.Order
	}
	if step.Separator != "" {
		m["separator"] = step.Separator
	}
	if step.Script != "" {
		m["js"] = step.Script
	}
	if step.RemoveImages {
		m["remove_images"] = true
	}
	if step.RemoveLinks {
		m["remove_links"] = true
	}
	return m
}

// runSkill executes a skill spec. If the skill has executable scripts in its
// installed directory, it finds and runs the main script. Otherwise, it outputs
// the SKILL.md instructions content.
//
// C3.9: When sandbox config is present on the spec, the script runs in safe
// mode via the sandbox backend. When sandbox config is missing, it runs in
// unsafe mode with env scrubbing and sensitive path blocking.
func runSkill(spec *models.ToolSpec, action string, params map[string]string) ([]byte, error) {
	// Find the installed skill directory
	skillDir := filepath.Join(".claude", "skills", spec.Name)
	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		// Fallback: check if there are instructions to display
		if spec.Instructions != "" {
			return []byte(spec.Instructions), nil
		}
		return nil, fmt.Errorf("skill %q is not installed. Run: clictl install %s", spec.Name, spec.Name)
	}

	// Look for executable scripts in the skill directory
	scriptPath := findSkillScript(skillDir, action)
	if scriptPath != "" {
		// Build command args from params
		var cmdArgs []string
		for k, v := range params {
			if strings.HasPrefix(k, "__") {
				continue // skip internal flags
			}
			cmdArgs = append(cmdArgs, "--"+k, v)
		}

		// C3.9: Check if sandbox should be used
		if spec.Sandbox != nil {
			// Safe mode: run through sandbox backend
			sandboxCfg := buildSandboxConfig(spec)
			sandboxCfg.WorkingDir = skillDir

			backend := sandboxBackend.SelectBackend()
			command := scriptPath
			if strings.HasSuffix(scriptPath, ".py") {
				command = "python3"
				cmdArgs = append([]string{scriptPath}, cmdArgs...)
			}

			result, err := backend.Run(sandboxCfg, command, cmdArgs)
			if err != nil {
				return nil, fmt.Errorf("sandboxed execution failed: %w", err)
			}

			// Log any violations
			for _, v := range result.Violations {
				logViolation(v)
			}

			if result.ExitCode != 0 {
				return nil, fmt.Errorf("script exited with code %d: %s", result.ExitCode, string(result.Stderr))
			}

			return result.Stdout, nil
		}

		// Unsafe mode: direct execution with env scrubbing
		cmd := execCommand(scriptPath, cmdArgs...)
		cmd.Dir = skillDir
		cmd.Stderr = os.Stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("running script %s: %w", filepath.Base(scriptPath), err)
		}
		return out, nil
	}

	// No executable script found. Read SKILL.md as instruction output.
	skillMD := filepath.Join(skillDir, "SKILL.md")
	if data, err := os.ReadFile(skillMD); err == nil {
		return data, nil
	}

	// Last resort: show spec instructions
	if spec.Instructions != "" {
		return []byte(spec.Instructions), nil
	}

	return nil, fmt.Errorf("skill %q has no scripts or instructions to execute", spec.Name)
}

// findSkillScript searches the skill directory for an executable script matching
// the action name. Falls back to common entry point names.
func findSkillScript(dir string, action string) string {
	// Try action-specific script first
	candidates := []string{
		action + ".sh",
		action + ".py",
		action,
		"run.sh",
		"run.py",
		"main.sh",
		"main.py",
	}

	for _, name := range candidates {
		p := filepath.Join(dir, name)
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		// Check if executable (owner execute bit)
		if info.Mode()&0o111 != 0 {
			return p
		}
		// Python scripts can be run via interpreter even without execute bit
		if strings.HasSuffix(name, ".py") {
			return p
		}
	}
	return ""
}

// execCommand creates an exec.Cmd for running a script. Python scripts
// are dispatched through the python3 interpreter automatically.
func execCommand(script string, args ...string) *exec.Cmd {
	if strings.HasSuffix(script, ".py") {
		return exec.Command("python3", append([]string{script}, args...)...)
	}
	return exec.Command(script, args...)
}

func init() {
	rootCmd.AddCommand(execCmd)
}
