// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/registry"
	"github.com/clictl/cli/internal/signing"
)

var verifyAll bool

var verifyCmd = &cobra.Command{
	Use:   "verify [tool]",
	Short: "Verify tool integrity, version drift, and yanked status",
	Long: `Verify installed tools against the registry. Checks:

  - Content integrity (etag comparison against registry and lock file)
  - Version drift (installed version vs registry latest)
  - Yanked versions (tools or dependencies that have been yanked)
  - Spec signatures (if present)

  clictl verify github-mcp          # verify a single tool
  clictl verify --all               # verify all installed tools

Exit code 0 if all tools verified, 1 if any issues detected.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !verifyAll && len(args) == 0 {
			return fmt.Errorf("provide a tool name or use --all to verify all installed tools")
		}

		ctx := cmd.Context()

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)

		cache := registry.NewCache(cfg.CacheDir)
		client := registry.NewClient(cfg.APIURL, cache, true) // always bypass cache for verification

		token := config.ResolveAuthToken(flagAPIKey, cfg)
		if token != "" {
			client.AuthToken = token
		}

		// Determine which tools to verify
		var tools []string
		if verifyAll {
			tools = loadInstalled()
			if len(tools) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No tools installed.")
				return nil
			}
		} else {
			tools = []string{args[0]}
		}

		// Load lock file once
		lockFile, lockErr := LoadLockFile()
		if lockErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not read lock file: %v\n", lockErr)
		}

		bucketsDir := config.ToolboxesDir()

		type verifyResult struct {
			name              string
			version           string
			status            string // "verified (registry etag matches)" or "WARNING: ..."
			signature         string // "verified", "unsigned", or "invalid"
			transparencyLog   string // "verified (index #N, time)", "not available", "offline"
			versionDrift      string // "up to date", "update available (vX.Y.Z)", "not found in registry"
			yankedStatus      string // "ok", "yanked", "yanked dependency: X"
			warnings          []string
			ok                bool
		}

		var results []verifyResult

		for _, toolName := range tools {
			// Fetch the latest spec from the registry (bypassing cache)
			registrySpec, registryYAML, fetchErr := client.GetSpecYAML(ctx, toolName)
			if fetchErr != nil {
				results = append(results, verifyResult{
					name:   toolName,
					status: fmt.Sprintf("ERROR: could not fetch from registry - %v", fetchErr),
					ok:     false,
				})
				continue
			}

			registryETag := computeETag(registryYAML)
			version := registrySpec.Version
			toolOK := true

			// Check against local cache
			localCache := registry.NewCache(cfg.CacheDir)
			localData, localETag, _, localErr := localCache.Get(toolName)

			cacheStatus := ""
			if localErr != nil || localData == nil {
				cacheStatus = "no local cache"
			} else {
				localComputedETag := computeETag(localData)
				if localComputedETag == registryETag {
					cacheStatus = "registry etag matches"
				} else {
					cacheStatus = fmt.Sprintf("content mismatch (local: %.12s, registry: %.12s)", localComputedETag, registryETag)
					toolOK = false
					_ = localETag // suppress unused warning
				}
			}

			// Check against lock file (etag + content_sha256)
			lockStatus := ""
			if lockFile != nil {
				if entry, ok := lockFile.Tools[toolName]; ok {
					if entry.ETag == registryETag {
						lockStatus = "lock file etag matches"
					} else {
						lockStatus = fmt.Sprintf("lock file mismatch (lock: %.12s, registry: %.12s)", entry.ETag, registryETag)
						toolOK = false
					}
					// P10.13: Verify content_sha256 if present in lock file
					if entry.ContentSHA256 != "" && registryYAML != nil {
						actualHash := computeContentSHA256(registryYAML)
						if actualHash != entry.ContentSHA256 {
							lockStatus += fmt.Sprintf("; content hash mismatch (lock: %.12s, actual: %.12s)", entry.ContentSHA256, actualHash)
							toolOK = false
						}
					}
				} else {
					lockStatus = "not in lock file"
				}
			}

			// Version drift check: compare installed version with registry latest
			installedVersion := ""
			if lockFile != nil {
				if entry, inLock := lockFile.Tools[toolName]; inLock {
					installedVersion = entry.Version
				}
			}
			if installedVersion == "" {
				localSpec, resolveErr := registry.ResolveSpec(ctx, toolName, cfg, cache, false)
				if resolveErr == nil {
					installedVersion = localSpec.Version
				}
			}

			versionDrift := ""
			registryVersion := ""
			for _, reg := range cfg.Toolboxes {
				regDir := filepath.Join(bucketsDir, reg.Name)
				li := registry.NewLocalIndex(regDir, reg.Name)
				entry, entryErr := li.GetEntry(toolName)
				if entryErr != nil {
					continue
				}
				registryVersion = entry.Version
				break
			}
			if registryVersion == "" {
				registryVersion = version
			}

			if installedVersion == "" {
				versionDrift = "unknown installed version"
			} else if installedVersion == registryVersion {
				versionDrift = "up to date"
			} else {
				versionDrift = fmt.Sprintf("update available (%s -> %s)", installedVersion, registryVersion)
				// Version drift is informational, not a failure
			}

			// Deprecated version check
			yankedStatus := "ok"
			if registrySpec.Deprecated {
				yankedStatus = "deprecated"
				toolOK = false
			}

			// Check deprecated dependencies
			for _, dep := range registrySpec.Depends {
				depSpec, _, depErr := client.GetSpecYAML(ctx, dep)
				if depErr != nil {
					continue
				}
				if depSpec.Deprecated {
					yankedStatus = fmt.Sprintf("deprecated dependency: %s", dep)
					toolOK = false
					break
				}
			}

			// Integrity verification via per-file SHA256 hashes
			sigStatus := "unsigned"
			if registrySpec.Source != nil && len(registrySpec.Source.Files) > 0 {
				hasHashes := false
				for _, f := range registrySpec.Source.Files {
					if f.SHA256 != "" {
						hasHashes = true
						break
					}
				}
				if hasHashes {
					sigErr := verifySkillSignature(registrySpec, registryYAML, client)
					if sigErr != nil {
						sigStatus = "invalid"
						toolOK = false
					} else {
						sigStatus = "verified"
					}
				}
			}

			// Sigstore transparency log verification
			transparencyStatus := "not available"
			if token != "" {
				bundleData, bundleErr := client.GetSigstoreBundle(ctx, toolName)
				if bundleErr == nil && bundleData != nil {
					bundle, parseErr := signing.ParseSigstoreBundle(bundleData)
					if parseErr == nil {
						expectedIdentity := signing.ResolveSigstoreIdentity(ctx, cfg.APIURL)
						result, verifyErr := signing.VerifySigstoreBundle(string(registryYAML), *bundle, expectedIdentity)
						if verifyErr == nil && result.Verified {
							logTime := result.IntegratedTime.UTC().Format(time.RFC3339)
							transparencyStatus = fmt.Sprintf("verified (index #%d, %s)", result.LogIndex, logTime)
						} else if verifyErr != nil {
							transparencyStatus = fmt.Sprintf("invalid (%v)", verifyErr)
						}
					}
				}
			}

			// P6.11: Collect additional warnings
			var warnings []string

			// Warn if tool is delisted (deprecated)
			if registrySpec.Deprecated {
				warnings = append(warnings, "DELISTED: this tool has been deprecated by its publisher")
				if registrySpec.DeprecatedBy != "" {
					warnings = append(warnings, fmt.Sprintf("  Replacement: %s", registrySpec.DeprecatedBy))
				}
			}

			// Warn if pack is missing (source files exist but no signed pack)
			if registrySpec.Source != nil && len(registrySpec.Source.Files) > 0 && sigStatus == "unsigned" {
				warnings = append(warnings, "MISSING PACK: source files declared but no signed pack available")
			}

			// Warn if verification is lapsed (transparency log not available for a verified publisher)
			if registrySpec.IsVerified && transparencyStatus == "not available" {
				warnings = append(warnings, "LAPSED VERIFICATION: publisher is verified but no transparency log entry found")
			}

			// Build final status string
			status := ""
			if toolOK {
				status = "verified (" + cacheStatus + ")"
			} else {
				parts := []string{}
				if cacheStatus != "" && cacheStatus != "registry etag matches" && cacheStatus != "no local cache" {
					parts = append(parts, cacheStatus)
				}
				if lockStatus != "" && lockStatus != "lock file etag matches" && lockStatus != "not in lock file" {
					parts = append(parts, lockStatus)
				}
				warning := "WARNING:"
				for _, p := range parts {
					warning += " " + p + ";"
				}
				status = warning
			}

			results = append(results, verifyResult{
				name:            toolName,
				version:         version,
				status:          status,
				signature:       sigStatus,
				transparencyLog: transparencyStatus,
				versionDrift:    versionDrift,
				yankedStatus:    yankedStatus,
				warnings:        warnings,
				ok:              toolOK,
			})
		}

		// Output results
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tVERSION\tINTEGRITY\tSIGNATURE\tTRANSPARENCY LOG\tVERSION DRIFT\tYANKED")
		hasMismatch := false
		for _, r := range results {
			versionStr := ""
			if r.version != "" {
				versionStr = "v" + r.version
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.name, versionStr, r.status, r.signature, r.transparencyLog, r.versionDrift, r.yankedStatus)
			if !r.ok {
				hasMismatch = true
			}
		}
		if err := w.Flush(); err != nil {
			return err
		}

		// P6.11: Print warnings after the table
		hasWarnings := false
		for _, r := range results {
			if len(r.warnings) > 0 {
				if !hasWarnings {
					fmt.Fprintln(cmd.OutOrStdout(), "\nWarnings:")
					hasWarnings = true
				}
				for _, warn := range r.warnings {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s\n", r.name, warn)
				}
			}
		}

		if hasMismatch {
			os.Exit(1)
		}

		return nil
	},
}

func init() {
	verifyCmd.Flags().BoolVar(&verifyAll, "all", false, "Verify all installed tools")
	rootCmd.AddCommand(verifyCmd)
}
