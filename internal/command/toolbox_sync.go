// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/clictl/cli/internal/config"
	"github.com/spf13/cobra"
)

var (
	flagSyncDryRun bool
	flagSyncForce  bool
)

// syncPayload is the JSON body sent to the toolbox-sync API endpoint.
type syncPayload struct {
	Source    syncSource    `json:"source"`
	Tools     []ScannedTool `json:"tools"`
	Namespace string        `json:"namespace,omitempty"`
}

// syncSource identifies the git repository and ref that produced the tools.
type syncSource struct {
	Repo   string `json:"repo"`
	Ref    string `json:"ref"`
	Commit string `json:"commit"`
}

// syncResponse is the JSON body returned by the toolbox-sync API endpoint.
type syncResponse struct {
	Added   int `json:"added"`
	Updated int `json:"updated"`
	Pruned  int `json:"pruned"`
	Total   int `json:"total"`
}

var toolboxSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Scan local specs and push the index to the clictl API",
	Long: `Scan the current repository for tool spec YAML files, compute metadata
and SHA256 hashes, then push the index to the clictl API.

Reads .clictl.yaml from the current directory (or git root) for workspace,
namespace, spec paths, and allowed branches.

Requires authentication via CLICTL_API_KEY or a logged-in session.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		// 1. Load .clictl.yaml
		projCfg, err := LoadCliCtlConfig()
		if err != nil {
			return fmt.Errorf("loading project config: %w", err)
		}

		// 2. Check current branch against allowed list.
		branch, err := gitCurrentBranch()
		if err != nil {
			return fmt.Errorf("detecting git branch: %w", err)
		}

		if !isBranchAllowed(branch, projCfg.Branches) {
			if !flagSyncForce {
				return fmt.Errorf(
					"branch %q is not in the allowed list %v (use --force to override)",
					branch, projCfg.Branches,
				)
			}
			fmt.Printf("Warning: branch %q is not in allowed list %v, proceeding with --force\n",
				branch, projCfg.Branches)
		}

		commitSHA, err := gitCurrentCommit()
		if err != nil {
			return fmt.Errorf("detecting git commit: %w", err)
		}

		repoURL, err := gitRemoteOriginURL()
		if err != nil {
			return fmt.Errorf("detecting git remote: %w", err)
		}

		// 3. Scan spec paths.
		tools, err := ScanSpecs(projCfg.SpecPaths)
		if err != nil {
			return fmt.Errorf("scanning specs: %w", err)
		}

		if len(tools) == 0 {
			fmt.Println("No tool specs found in configured paths.")
			return nil
		}

		// 4. Build payload.
		payload := syncPayload{
			Source: syncSource{
				Repo:   repoURL,
				Ref:    branch,
				Commit: commitSHA,
			},
			Tools:     tools,
			Namespace: projCfg.Namespace,
		}

		// 5. Dry-run: print what would be synced and exit.
		if flagSyncDryRun {
			fmt.Printf("Dry run: would sync %d tool(s) from branch %q\n", len(tools), branch)
			fmt.Printf("  Repo:   %s\n", repoURL)
			fmt.Printf("  Commit: %s\n", commitSHA)
			for _, t := range tools {
				fmt.Printf("  - %s@%s (%s) [%s]\n", t.Name, t.Version, t.Path, t.SHA256[:12])
			}
			return nil
		}

		// 6. Resolve auth and workspace.
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg.APIURL = config.ResolveAPIURL(flagAPIURL, cfg)
		token := config.ResolveAuthToken(flagAPIKey, cfg)
		if token == "" {
			return fmt.Errorf("authentication required (set CLICTL_API_KEY or run clictl login)")
		}

		workspace := cfg.Auth.ActiveWorkspace
		if workspace == "" {
			return fmt.Errorf("no workspace configured (run clictl login or clictl workspace switch)")
		}

		// 7. POST to the toolbox-sync endpoint.
		result, err := postToolboxSync(ctx, cfg.APIURL, workspace, token, payload)
		if err != nil {
			return err
		}

		fmt.Printf("Synced %d tools (%d added, %d updated, %d pruned)\n",
			result.Total, result.Added, result.Updated, result.Pruned)
		return nil
	},
}

// postToolboxSync sends the sync payload to the API and returns the result.
func postToolboxSync(ctx context.Context, apiURL, workspace, token string, payload syncPayload) (*syncResponse, error) {
	u := fmt.Sprintf("%s/api/v1/workspaces/%s/toolbox-sync/",
		apiURL, url.PathEscape(workspace))

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encoding payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("posting sync: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("sync failed: %d %s", resp.StatusCode, string(respBody))
	}

	var result syncResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &result, nil
}

// gitCurrentBranch returns the name of the currently checked-out branch.
func gitCurrentBranch() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// gitCurrentCommit returns the full SHA of the current HEAD commit.
func gitCurrentCommit() (string, error) {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// gitRemoteOriginURL returns the URL of the "origin" remote.
func gitRemoteOriginURL() (string, error) {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// isBranchAllowed checks whether branch is in the allowed list.
func isBranchAllowed(branch string, allowed []string) bool {
	for _, b := range allowed {
		if b == branch {
			return true
		}
	}
	return false
}

func init() {
	toolboxSyncCmd.Flags().BoolVar(&flagSyncDryRun, "dry-run", false, "Show what would be synced without pushing")
	toolboxSyncCmd.Flags().BoolVar(&flagSyncForce, "force", false, "Sync even if current branch is not in the allowed list")
	toolboxCmd.AddCommand(toolboxSyncCmd)
}
