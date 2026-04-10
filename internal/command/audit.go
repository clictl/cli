// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/registry"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Check installed tools for integrity, availability, and security issues",
	Long: `Audit installed tools against the registry. For each installed tool:

  - Checks if the source repository is still accessible
  - Compares SHA256 hash of current spec content against the index
  - Verifies against the lock file if present
  - Checks for known security advisories

Reports: "OK", "SHA256 mismatch", "source unavailable", or security warnings.

  clictl audit`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)

		installed := loadInstalled()
		if len(installed) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No tools installed.")
			return nil
		}

		cache := registry.NewCache(cfg.CacheDir)
		client := registry.NewClient(cfg.APIURL, cache, true) // bypass cache for audit

		token := config.ResolveAuthToken(flagAPIKey, cfg)
		if token != "" {
			client.AuthToken = token
		}

		// Load lock file for cross-reference
		lockFile, lockErr := LoadLockFile()
		if lockErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not read lock file: %v\n", lockErr)
		}

		type auditResult struct {
			name       string
			version    string
			sha256     string
			lockStatus string
			status     string
			ok         bool
		}

		var results []auditResult

		for _, toolName := range installed {
			result := auditTool(ctx, toolName, client, cfg, lockFile)
			results = append(results, result)
		}

		// Output integrity results
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tVERSION\tINTEGRITY\tLOCK FILE")
		hasMismatch := false
		for _, r := range results {
			versionStr := ""
			if r.version != "" {
				versionStr = "v" + r.version
			}
			lockStr := r.lockStatus
			if lockStr == "" {
				lockStr = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.name, versionStr, r.status, lockStr)
			if !r.ok {
				hasMismatch = true
			}
		}
		if err := w.Flush(); err != nil {
			return err
		}

		// Check security advisories (P8.5)
		advisories := fetchSecurityAdvisories(ctx, cfg.APIURL, installed)
		if len(advisories) > 0 {
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintf(cmd.OutOrStdout(), "Found %d security advisory(ies):\n\n", len(advisories))
			for _, adv := range advisories {
				cveStr := ""
				if adv.CVEID != "" {
					cveStr = fmt.Sprintf(" (%s)", adv.CVEID)
				}
				fixStr := ""
				if adv.FixedVersion != "" {
					fixStr = fmt.Sprintf(" - fix available in %s", adv.FixedVersion)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  [%s] %s: %s%s%s\n",
					adv.Severity, adv.ToolName, adv.Description, cveStr, fixStr)
			}
			hasMismatch = true
		}

		if hasMismatch {
			os.Exit(1)
		}

		return nil
	},
}

// auditTool checks a single installed tool against the registry and lock file.
func auditTool(ctx context.Context, toolName string, client *registry.Client, cfg *config.Config, lockFile *LockFile) struct {
	name       string
	version    string
	sha256     string
	lockStatus string
	status     string
	ok         bool
} {
	type auditResult = struct {
		name       string
		version    string
		sha256     string
		lockStatus string
		status     string
		ok         bool
	}

	// Fetch spec from registry
	spec, rawYAML, fetchErr := client.GetSpecYAML(ctx, toolName)
	if fetchErr != nil {
		return auditResult{
			name:   toolName,
			status: "source unavailable",
			ok:     false,
		}
	}

	currentHash := computeSHA256(rawYAML)

	// Check SHA256 against local index entries
	indexOK := true
	for _, reg := range cfg.Toolboxes {
		regDir := config.ToolboxesDir()
		li := registry.NewLocalIndex(regDir+"/"+reg.Name, reg.Name)
		entry, entryErr := li.GetEntry(toolName)
		if entryErr != nil {
			continue
		}
		if entry.SHA256 != "" && entry.SHA256 != currentHash {
			indexOK = false
		}
		break
	}

	// Check source_repo accessibility if the spec has source info
	sourceAccessible := true
	if spec.Source != nil && spec.Source.Repo != "" {
		sourceAccessible = checkRepoAccessible(spec.Source.Repo)
	}

	// Check against lock file
	lockStatus := ""
	if lockFile != nil {
		if entry, inLock := lockFile.Tools[toolName]; inLock {
			lockETag := computeETag(rawYAML)
			if entry.ETag == lockETag {
				lockStatus = "matches"
			} else {
				lockStatus = "mismatch"
				indexOK = false
			}
		} else {
			lockStatus = "not locked"
		}
	}

	status := "OK"
	ok := true
	if !sourceAccessible {
		status = "source unavailable"
		ok = false
	} else if !indexOK {
		status = "SHA256 mismatch"
		ok = false
	}

	return auditResult{
		name:       toolName,
		version:    spec.Version,
		sha256:     currentHash,
		lockStatus: lockStatus,
		status:     status,
		ok:         ok,
	}
}

// computeSHA256 returns the hex-encoded SHA256 hash of data.
func computeSHA256(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// checkRepoAccessible tests whether a GitHub repo is reachable via a HEAD request.
// Accepts "owner/repo" format and checks https://github.com/{owner}/{repo}.
func checkRepoAccessible(repo string) bool {
	url := "https://github.com/" + repo
	resp, err := http.Head(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode < 400
}

type securityAdvisory struct {
	ToolName         string `json:"tool_name"`
	Severity         string `json:"severity"`
	Description      string `json:"description"`
	AffectedVersions string `json:"affected_versions"`
	FixedVersion     string `json:"fixed_version"`
	CVEID            string `json:"cve_id"`
}

type securityAuditResponse struct {
	Advisories []securityAdvisory `json:"advisories"`
}

// fetchSecurityAdvisories checks installed tools against the security advisories API.
func fetchSecurityAdvisories(ctx context.Context, apiURL string, toolNames []string) []securityAdvisory {
	if len(toolNames) == 0 {
		return nil
	}

	// Build comma-separated tool list
	toolsParam := ""
	for i, name := range toolNames {
		if i > 0 {
			toolsParam += ","
		}
		toolsParam += name
	}

	url := fmt.Sprintf("%s/api/v1/security/audit/?tools=%s", apiURL, toolsParam)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var result securityAuditResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}

	return result.Advisories
}

func init() {
	rootCmd.AddCommand(auditCmd)
}
