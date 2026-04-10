// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/registry"
)

var publishPublic bool

var publishCmd = &cobra.Command{
	Use:   "publish <spec.yaml|pack.tar.gz>",
	Short: "Publish a spec or pack to the platform",
	Long: `Publish a tool spec or pack archive to the clictl platform.

  # Publish a spec to your workspace registry
  clictl publish my-api.yaml

  # Publish a pack archive
  clictl publish my-skill-1.0.0.tar.gz

  # Submit to the official registry (creates a review request)
  clictl publish my-api.yaml --public

Requires login.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		publishFile := args[0]

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		token := config.ResolveAuthToken(flagAPIKey, cfg)
		if token == "" {
			return fmt.Errorf("login required: run 'clictl login' first")
		}

		// P2.33: detect pack archive vs spec YAML
		if strings.HasSuffix(publishFile, ".tar.gz") || strings.HasSuffix(publishFile, ".tgz") {
			return publishPack(cmd, cfg, token, publishFile)
		}

		return publishSpec(cmd, cfg, token, publishFile)
	},
}

// publishSpec publishes a YAML spec file to the platform.
func publishSpec(cmd *cobra.Command, cfg *config.Config, token, specFile string) error {
	data, err := os.ReadFile(specFile)
	if err != nil {
		return fmt.Errorf("reading spec file: %w", err)
	}

	// Validate the spec by parsing it
	spec, err := registry.ParseSpec(data)
	if err != nil {
		return fmt.Errorf("invalid spec: %w", err)
	}

	// Validate required fields
	if spec.Name == "" {
		return fmt.Errorf("spec must have a 'name' field")
	}
	if spec.Description == "" {
		return fmt.Errorf("spec must have a 'description' field")
	}
	if spec.Version == "" {
		return fmt.Errorf("spec must have a 'version' field")
	}

	apiURL := config.ResolveAPIURL(flagAPIURL, cfg)

	payload := map[string]interface{}{
		"name":      spec.Name,
		"version":   spec.Version,
		"spec_yaml": string(data),
		"public":    publishPublic,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding payload: %w", err)
	}

	// N2.12: POST to workspace tools/create/ endpoint when workspace is active
	var u string
	if cfg.Auth.ActiveWorkspace != "" {
		u = fmt.Sprintf("%s/api/v1/workspaces/%s/tools/create/", apiURL, cfg.Auth.ActiveWorkspace)
	} else {
		u = fmt.Sprintf("%s/api/v1/specs/create/", apiURL)
	}
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "clictl/1.0")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("publishing spec: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("publish failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	displayName := spec.Name
	if spec.Namespace != "" {
		displayName = spec.Namespace + "/" + spec.Name
	}
	fmt.Printf("Published %s (v%s)\n", displayName, spec.Version)

	// Try to extract qualified_name from response
	var result map[string]interface{}
	if json.Unmarshal(respBody, &result) == nil {
		if qn, ok := result["qualified_name"].(string); ok && qn != "" {
			fmt.Printf("Qualified name: %s\n", qn)
		}
	}

	if publishPublic {
		fmt.Println("Submitted for review. It will appear in the public registry once approved.")
	}
	if cfg.Auth.ActiveWorkspace != "" {
		fmt.Printf("Workspace: %s\n", cfg.Auth.ActiveWorkspace)
	}

	return nil
}

// publishPack uploads a .tar.gz pack archive to the platform.
func publishPack(cmd *cobra.Command, cfg *config.Config, token, packFile string) error {
	info, err := os.Stat(packFile)
	if err != nil {
		return fmt.Errorf("reading pack file: %w", err)
	}

	// Enforce archive size limit
	if info.Size() > packMaxArchiveBytes {
		return fmt.Errorf("archive size %d bytes exceeds maximum of %d bytes (%d MiB)", info.Size(), packMaxArchiveBytes, packMaxArchiveBytes/(1024*1024))
	}

	f, err := os.Open(packFile)
	if err != nil {
		return fmt.Errorf("opening pack file: %w", err)
	}
	defer f.Close()

	// Build multipart form upload
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("pack", filepath.Base(packFile))
	if err != nil {
		return fmt.Errorf("creating form file: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return fmt.Errorf("copying pack data: %w", err)
	}

	if publishPublic {
		writer.WriteField("public", "true")
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("closing multipart writer: %w", err)
	}

	apiURL := config.ResolveAPIURL(flagAPIURL, cfg)

	var u string
	if cfg.Auth.ActiveWorkspace != "" {
		u = fmt.Sprintf("%s/api/v1/workspaces/%s/tools/create/", apiURL, cfg.Auth.ActiveWorkspace)
	} else {
		u = fmt.Sprintf("%s/api/v1/specs/create/", apiURL)
	}

	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, u, &buf)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "clictl/1.0")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("uploading pack: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("publish failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	fmt.Printf("Pack published: %s\n", filepath.Base(packFile))

	var result map[string]interface{}
	if json.Unmarshal(respBody, &result) == nil {
		if name, ok := result["name"].(string); ok {
			fmt.Printf("  Name: %s\n", name)
		}
		if version, ok := result["version"].(string); ok {
			fmt.Printf("  Version: %s\n", version)
		}
		if qn, ok := result["qualified_name"].(string); ok && qn != "" {
			fmt.Printf("  Qualified name: %s\n", qn)
		}
	}

	if publishPublic {
		fmt.Println("Submitted for review. It will appear in the public registry once approved.")
	}
	if cfg.Auth.ActiveWorkspace != "" {
		fmt.Printf("Workspace: %s\n", cfg.Auth.ActiveWorkspace)
	}

	return nil
}

func init() {
	publishCmd.Flags().BoolVar(&publishPublic, "public", false, "Submit to the official public registry for review")
	rootCmd.AddCommand(publishCmd)
}
