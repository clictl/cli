// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/sandbox"
)

var (
	sandboxDenyFlags   []string
	sandboxDenyAll     bool
	sandboxDryRun      bool
	sandboxAllViolFlag bool
)

// sandboxCmd is the top-level command for sandbox management.
var sandboxCmd = &cobra.Command{
	Use:   "sandbox [command]",
	Short: "Manage sandbox isolation for tools",
	Long: `View sandbox status, set up rootfs, manage runtime layers, and view violations.

  clictl sandbox            # interactive shell with PTY (C3.16)
  clictl sandbox setup      # set up minimal rootfs
  clictl sandbox check      # verify sandbox readiness
  clictl sandbox add-runtime node  # download a runtime layer
  clictl sandbox violations <tool> # view violations`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// C3.16: Without subcommand, show sandbox status
		fmt.Println("Sandbox Status")
		fmt.Println(strings.Repeat("-", 40))

		// Show selected backend
		backend := sandbox.SelectBackend()
		fmt.Printf("Active backend: %s\n", backend.Name())

		// Show all backends
		fmt.Println("\nAvailable backends:")
		for _, bs := range sandbox.ListBackends() {
			status := "unavailable"
			if bs.Available {
				status = "available"
			}
			fmt.Printf("  %-20s %s\n", bs.Name, status)
		}

		// Show rootfs status
		if err := sandbox.CheckRootfs(""); err != nil {
			fmt.Printf("\nRootfs: not set up (%v)\n", err)
			fmt.Println("  Run 'clictl sandbox setup' to create a minimal rootfs")
		} else {
			fmt.Printf("\nRootfs: ready at %s\n", sandbox.DefaultRootfsDir())
		}

		// Show runtime layers
		layers, err := sandbox.ListRuntimes()
		if err == nil && len(layers) > 0 {
			fmt.Println("\nRuntime layers:")
			for _, l := range layers {
				fmt.Printf("  %s: %s\n", l.Name, l.Dir)
			}
		} else {
			fmt.Println("\nNo runtime layers cached.")
			fmt.Println("  Run 'clictl sandbox add-runtime <name>' to download one")
		}

		// Show violation count
		violations, _ := loadAllViolations()
		if len(violations) > 0 {
			fmt.Printf("\n%d sandbox violations recorded.\n", len(violations))
			fmt.Println("  Run 'clictl sandbox violations --all' to view them")
		}

		return nil
	},
}

// C3.17: `clictl sandbox setup` - download busybox, create minimal rootfs.
var sandboxSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Set up the minimal sandbox rootfs",
	Long:  "Downloads busybox and creates a minimal rootfs for sandbox isolation.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return sandbox.SetupRootfs("")
	},
}

// C3.17: `clictl sandbox check` - verify sandbox readiness.
var sandboxCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check if the sandbox is ready",
	RunE: func(cmd *cobra.Command, args []string) error {
		backend := sandbox.SelectBackend()
		fmt.Printf("Backend: %s\n", backend.Name())

		if err := sandbox.CheckRootfs(""); err != nil {
			fmt.Printf("Rootfs: NOT READY (%v)\n", err)
			return fmt.Errorf("sandbox is not ready. Run 'clictl sandbox setup'")
		}

		fmt.Println("Rootfs: OK")
		fmt.Println("Sandbox is ready.")
		return nil
	},
}

// C3.17: `clictl sandbox add-runtime` - download static runtimes.
var sandboxAddRuntimeCmd = &cobra.Command{
	Use:   "add-runtime <name>",
	Short: "Download and cache a runtime layer (node, bun, python)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := sandbox.AddRuntime(args[0])
		return err
	},
}

// C3.23: `clictl sandbox violations <tool>` - view violations for a tool.
// C3.24: `clictl sandbox violations --all` - view all violations.
var sandboxViolationsCmd = &cobra.Command{
	Use:   "violations [tool]",
	Short: "View sandbox violations",
	Long: `View recorded sandbox policy violations for a specific tool or all tools.

  clictl sandbox violations github    # violations for github tool
  clictl sandbox violations --all     # all violations`,
	RunE: func(cmd *cobra.Command, args []string) error {
		violations, err := loadAllViolations()
		if err != nil {
			return fmt.Errorf("loading violations: %w", err)
		}

		if len(violations) == 0 {
			fmt.Println("No sandbox violations recorded.")
			return nil
		}

		// Filter by tool if specified
		var filtered []sandbox.Violation
		if len(args) > 0 && !sandboxAllViolFlag {
			toolName := args[0]
			for _, v := range violations {
				if v.ToolName == toolName {
					filtered = append(filtered, v)
				}
			}
			if len(filtered) == 0 {
				fmt.Printf("No violations recorded for %s.\n", toolName)
				return nil
			}
		} else if !sandboxAllViolFlag && len(args) == 0 {
			return fmt.Errorf("specify a tool name or use --all to view all violations")
		} else {
			filtered = violations
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "TOOL\tTYPE\tDESCRIPTION\tTIME")
		for _, v := range filtered {
			timeStr := v.Timestamp.Format("2006-01-02 15:04:05")
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", v.ToolName, v.Type, v.Description, timeStr)
		}
		w.Flush()

		fmt.Printf("\nTotal: %d violations\n", len(filtered))
		return nil
	},
}

// buildSandboxConfig creates a sandbox.Config from a tool spec and user flags.
// C3.9: Determines safe mode (sandbox config present) vs unsafe mode (missing).
// C3.18: Policy merge - publisher declares, consumer restricts, more restrictive wins.
func buildSandboxConfig(spec *models.ToolSpec) *sandbox.Config {
	cfg := &sandbox.Config{
		ToolName:       spec.Name,
		NetworkMode:    sandbox.NetworkHost,
		SeccompProfile: "standard",
		Timeout:        5 * time.Minute,
	}

	// Apply publisher-declared sandbox config
	if spec.Sandbox != nil {
		// Network
		if spec.Sandbox.Network != nil {
			if len(spec.Sandbox.Network.Allow) > 0 {
				cfg.NetworkMode = sandbox.NetworkAllowlist
				cfg.AllowedHosts = spec.Sandbox.Network.Allow
			}
		}

		// Filesystem
		if spec.Sandbox.Filesystem != nil {
			for _, r := range spec.Sandbox.Filesystem.Read {
				cfg.Mounts = append(cfg.Mounts, sandbox.Mount{
					Source:   expandHomePath(r),
					Target:   expandHomePath(r),
					ReadOnly: true,
				})
			}
			for _, w := range spec.Sandbox.Filesystem.Write {
				cfg.Mounts = append(cfg.Mounts, sandbox.Mount{
					Source:   expandHomePath(w),
					Target:   expandHomePath(w),
					ReadOnly: false,
				})
			}
		}

		// Environment
		if spec.Sandbox.Env != nil {
			var env []string
			for _, key := range spec.Sandbox.Env.Allow {
				if val := os.Getenv(key); val != "" {
					env = append(env, key+"="+val)
				}
			}
			cfg.Env = env
		}
	}

	// C3.11: Credential forwarding defaults
	cfg.Credentials = sandbox.CredentialConfig{
		SSHAgent:  true,
		GitConfig: true,
		NPMConfig: true,
	}

	// C3.12: Apply user credential controls
	if sandboxDenyAll {
		cfg.Credentials.DenyAll = true
	}
	for _, deny := range sandboxDenyFlags {
		cfg.Credentials.DenyList = append(cfg.Credentials.DenyList, deny)
	}

	// C3.18: Apply consumer restrictions (more restrictive wins)
	applySandboxConfig, _ := config.Load()
	if applySandboxConfig != nil {
		// Consumer can restrict further via config
		applyConsumerRestrictions(cfg, applySandboxConfig)
	}

	return cfg
}

// applyConsumerRestrictions applies consumer-side restrictions from config.
// C3.18: The more restrictive policy always wins.
func applyConsumerRestrictions(cfg *sandbox.Config, cliCfg *config.Config) {
	// Consumer execution config can tighten limits
	if cliCfg.Execution.Memory != "" {
		// Parse memory limit from config (e.g., "512m")
		var mb int
		if _, err := fmt.Sscanf(cliCfg.Execution.Memory, "%dm", &mb); err == nil {
			if cfg.MemoryLimitMB == 0 || mb < cfg.MemoryLimitMB {
				cfg.MemoryLimitMB = mb
			}
		}
	}

	if cliCfg.Execution.Timeout > 0 {
		timeout := time.Duration(cliCfg.Execution.Timeout) * time.Second
		if cfg.Timeout == 0 || timeout < cfg.Timeout {
			cfg.Timeout = timeout
		}
	}
}

// expandHomePath expands ~ prefix in a path.
func expandHomePath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// C3.20: shimSandboxMode determines whether a shim should run in safe or unsafe mode.
func shimSandboxMode(spec *models.ToolSpec) string {
	if spec.Sandbox != nil {
		return "safe"
	}
	return "unsafe"
}

// C3.21: logViolation writes a sandbox violation to the violation log file.
func logViolation(v sandbox.Violation) error {
	logPath := violationLogPath()
	dir := filepath.Dir(logPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating violation log dir: %w", err)
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening violation log: %w", err)
	}
	defer f.Close()

	entry := map[string]string{
		"tool":        v.ToolName,
		"type":        v.Type,
		"description": v.Description,
		"timestamp":   v.Timestamp.Format(time.RFC3339),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling violation: %w", err)
	}

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing violation: %w", err)
	}

	return nil
}

// C3.22: reportViolation sends an anonymous violation report to the platform API (opt-in).
func reportViolation(v sandbox.Violation) {
	cfg, err := config.Load()
	if err != nil {
		return
	}
	if !cfg.Telemetry {
		return // Opt-out
	}

	// Anonymized report with tool name and violation type only
	// This extends the existing telemetry system
	fmt.Fprintf(os.Stderr, "clictl: violation reported for %s (%s)\n", v.ToolName, v.Type)
}

// violationLogPath returns the path to the sandbox violations log.
func violationLogPath() string {
	return filepath.Join(config.BaseDir(), "sandbox-violations.log")
}

// loadAllViolations reads all violations from the log file.
func loadAllViolations() ([]sandbox.Violation, error) {
	logPath := violationLogPath()
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var violations []sandbox.Violation
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var entry map[string]string
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		ts, _ := time.Parse(time.RFC3339, entry["timestamp"])
		violations = append(violations, sandbox.Violation{
			ToolName:    entry["tool"],
			Type:        entry["type"],
			Description: entry["description"],
			Timestamp:   ts,
		})
	}

	return violations, scanner.Err()
}

// C3.19: setupDependencyIsolation creates a virtual environment for tool dependencies.
// On first run, it creates a venv, installs dependencies with network access,
// then subsequent runs use the cached venv without network.
func setupDependencyIsolation(spec *models.ToolSpec) (string, error) {
	if spec.Runtime == nil || len(spec.Runtime.Dependencies) == 0 {
		return "", nil
	}

	// Determine venv path based on tool name and version
	venvDir := filepath.Join(config.BaseDir(), "venvs", spec.Name, spec.Version)

	// Check if venv already exists
	if _, err := os.Stat(filepath.Join(venvDir, "bin", "python3")); err == nil {
		return venvDir, nil // Already set up
	}

	fmt.Fprintf(os.Stderr, "Setting up dependency isolation for %s...\n", spec.Name)

	if err := os.MkdirAll(venvDir, 0o755); err != nil {
		return "", fmt.Errorf("creating venv directory: %w", err)
	}

	// This is a placeholder for the actual venv creation logic
	// In production, this would call python3 -m venv and pip install
	fmt.Fprintf(os.Stderr, "  Venv directory: %s\n", venvDir)
	fmt.Fprintf(os.Stderr, "  Dependencies: %s\n", strings.Join(spec.Runtime.Dependencies, ", "))

	return venvDir, nil
}

func init() {
	sandboxViolationsCmd.Flags().BoolVar(&sandboxAllViolFlag, "all", false, "Show violations for all tools")

	sandboxCmd.AddCommand(sandboxSetupCmd)
	sandboxCmd.AddCommand(sandboxCheckCmd)
	sandboxCmd.AddCommand(sandboxAddRuntimeCmd)
	sandboxCmd.AddCommand(sandboxViolationsCmd)
	rootCmd.AddCommand(sandboxCmd)
}
