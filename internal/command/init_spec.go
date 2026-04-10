// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package command

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var initFromURL string

var initSpecCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a new tool spec interactively",
	Long: `Scaffold a new clictl tool spec file interactively or from an OpenAPI URL.

  # Interactive mode
  clictl init

  # From an OpenAPI spec URL
  clictl init --from https://api.example.com/openapi.json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if initFromURL != "" {
			return initFromOpenAPI(initFromURL)
		}
		return initInteractive()
	},
}

// kebabCaseRegexp validates kebab-case names.
var kebabCaseRegexp = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

func initInteractive() error {
	reader := bufio.NewReader(os.Stdin)

	// Tool name
	name := promptString(reader, "Tool name (kebab-case)", "")
	if name == "" {
		return fmt.Errorf("tool name is required")
	}
	if !kebabCaseRegexp.MatchString(name) {
		return fmt.Errorf("tool name must be kebab-case (e.g., my-api)")
	}

	// Description
	description := promptString(reader, "Description (one line)", "")
	if description == "" {
		return fmt.Errorf("description is required")
	}

	// Protocol
	toolType := promptChoice(reader, "Protocol", []string{"http", "mcp", "skill", "website", "command"})

	// Category
	categories := []string{
		"ai", "analytics", "automation", "cloud", "communication",
		"data", "developer", "devops", "finance", "marketing",
		"monitoring", "productivity", "security", "social", "storage",
	}
	category := promptChoice(reader, "Category", categories)

	// Build spec
	spec := map[string]any{
		"spec":        "1.0",
		"name":        name,
		"protocol":    toolType,
		"description": description,
		"version":     "1.0",
		"category":    category,
	}

	switch toolType {
	case "http":
		baseURL := promptString(reader, "Base URL (e.g., https://api.example.com)", "")
		if baseURL == "" {
			return fmt.Errorf("base URL is required for HTTP tools")
		}
		spec["server"] = map[string]any{
			"url": baseURL,
		}
	case "mcp":
		registry := promptChoice(reader, "Package registry", []string{"npm", "pypi", "none"})
		if registry != "none" {
			pkgName := promptString(reader, "Package name", "")
			pkgVersion := promptString(reader, "Package version", "")
			spec["package"] = map[string]any{
				"registry": registry,
				"name":     pkgName,
				"version":  pkgVersion,
			}
		} else {
			command := promptString(reader, "Server command (e.g., npx)", "")
			args := promptString(reader, "Command args (e.g., @org/server)", "")
			srv := map[string]any{"command": command}
			if args != "" {
				srv["args"] = []string{args}
			}
			spec["server"] = srv
		}
	case "skill":
		repo := promptString(reader, "GitHub repo (org/repo)", "")
		path := promptString(reader, "Path within repo", "")
		src := map[string]any{"repo": repo}
		if path != "" {
			src["path"] = path
		}
		spec["source"] = src
	case "website":
		url := promptString(reader, "Website URL", "")
		if url != "" {
			spec["server"] = map[string]any{"url": url}
		}
	case "command":
		shell := promptString(reader, "Shell (e.g., bash)", "bash")
		if shell != "" {
			spec["server"] = map[string]any{"shell": shell}
		}
	}

	// Auth type
	authType := promptChoice(reader, "Auth type", []string{"none", "api_key", "bearer", "oauth2"})
	if authType != "none" {
		envVar := promptString(reader, "Environment variable for auth", strings.ToUpper(strings.ReplaceAll(name, "-", "_"))+"_API_KEY")
		auth := map[string]any{
			"type":    authType,
			"key_env": envVar,
		}
		switch authType {
		case "api_key":
			auth["inject"] = map[string]any{
				"location": "query",
				"param":    "api_key",
			}
		case "bearer":
			auth["inject"] = map[string]any{
				"location": "header",
				"param":    "Authorization",
				"prefix":   "Bearer ",
			}
		}
		spec["auth"] = auth
	}

	// First action
	fmt.Fprintf(os.Stderr, "\n--- First Action ---\n")
	actionName := promptString(reader, "Action name (e.g., list, get, search)", "")
	if actionName != "" {
		action := map[string]any{
			"name": actionName,
			"safe": true,
		}

		if toolType == "api" {
			method := promptChoice(reader, "HTTP method", []string{"GET", "POST", "PUT", "DELETE"})
			path := promptString(reader, "Path (e.g., /v1/items)", "")
			actionDesc := promptString(reader, "Action description", "")

			action["method"] = method
			action["path"] = path
			if actionDesc != "" {
				action["description"] = actionDesc
			}
			if method != "GET" {
				action["safe"] = false
			}
		} else if toolType == "cli" {
			runCmd := promptString(reader, "Run command template (e.g., docker ps {{.format}})", "")
			actionDesc := promptString(reader, "Action description", "")
			if runCmd != "" {
				action["run"] = runCmd
			}
			if actionDesc != "" {
				action["description"] = actionDesc
			}
		}

		spec["actions"] = []any{action}
	}

	// Determine output directory
	var dir string
	switch toolType {
	case "api":
		dir = filepath.Join("apis", category)
	case "cli":
		dir = filepath.Join("clis", category)
	case "website":
		dir = filepath.Join("websites", category)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	outPath := filepath.Join(dir, name+".yaml")

	data, err := yaml.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshaling spec: %w", err)
	}

	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return fmt.Errorf("writing spec: %w", err)
	}

	fmt.Printf("Created spec: %s\n", outPath)
	fmt.Println("Edit the file to add more actions and parameters.")
	return nil
}

func promptString(reader *bufio.Reader, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Fprintf(os.Stderr, "%s [%s]: ", label, defaultVal)
	} else {
		fmt.Fprintf(os.Stderr, "%s: ", label)
	}
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal
	}
	return line
}

func promptChoice(reader *bufio.Reader, label string, choices []string) string {
	fmt.Fprintf(os.Stderr, "%s:\n", label)
	for i, c := range choices {
		fmt.Fprintf(os.Stderr, "  [%d] %s\n", i+1, c)
	}
	fmt.Fprintf(os.Stderr, "Choice [1]: ")
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return choices[0]
	}
	var num int
	if _, err := fmt.Sscanf(line, "%d", &num); err == nil && num >= 1 && num <= len(choices) {
		return choices[num-1]
	}
	// Check if input matches a choice name directly
	for _, c := range choices {
		if strings.EqualFold(line, c) {
			return c
		}
	}
	return choices[0]
}

func initFromOpenAPI(url string) error {
	resp, err := SecureHTTPClient().Get(url)
	if err != nil {
		return fmt.Errorf("fetching OpenAPI spec: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("fetching OpenAPI spec: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading OpenAPI spec: %w", err)
	}

	var openAPI map[string]any
	if err := json.Unmarshal(body, &openAPI); err != nil {
		// Try YAML
		if yamlErr := yaml.Unmarshal(body, &openAPI); yamlErr != nil {
			return fmt.Errorf("parsing OpenAPI spec (tried JSON and YAML): %w", err)
		}
	}

	// Extract info
	info, _ := openAPI["info"].(map[string]any)
	title, _ := info["title"].(string)
	description, _ := info["description"].(string)
	version, _ := info["version"].(string)

	// Derive name from title
	name := strings.ToLower(strings.ReplaceAll(title, " ", "-"))
	name = regexp.MustCompile(`[^a-z0-9-]`).ReplaceAllString(name, "")
	if name == "" {
		name = "my-api"
	}

	// Extract servers for base URL
	var baseURL string
	if servers, ok := openAPI["servers"].([]any); ok && len(servers) > 0 {
		if server, ok := servers[0].(map[string]any); ok {
			baseURL, _ = server["url"].(string)
		}
	}

	// Build spec
	spec := map[string]any{
		"spec":        "1.0",
		"name":        name,
		"protocol":    "http",
		"description": description,
		"version":     version,
		"category":    "developer",
		"server": map[string]any{
			"url": baseURL,
		},
	}

	// Extract paths and build actions
	paths, _ := openAPI["paths"].(map[string]any)
	var actions []any
	for path, pathItem := range paths {
		methods, ok := pathItem.(map[string]any)
		if !ok {
			continue
		}
		for method, opData := range methods {
			method = strings.ToUpper(method)
			if method != "GET" && method != "POST" && method != "PUT" && method != "DELETE" && method != "PATCH" {
				continue
			}

			op, ok := opData.(map[string]any)
			if !ok {
				continue
			}

			opID, _ := op["operationId"].(string)
			summary, _ := op["summary"].(string)

			if opID == "" {
				// Generate an operation ID from method + path
				opID = strings.ToLower(method) + "-" + strings.ReplaceAll(strings.Trim(path, "/"), "/", "-")
				opID = regexp.MustCompile(`[^a-z0-9-]`).ReplaceAllString(opID, "")
			}

			action := map[string]any{
				"name":   opID,
				"method": method,
				"path":   path,
				"safe":   method == "GET",
			}
			if summary != "" {
				action["description"] = summary
			}

			// Extract parameters
			if params, ok := op["parameters"].([]any); ok {
				var specParams []any
				for _, p := range params {
					param, ok := p.(map[string]any)
					if !ok {
						continue
					}
					pName, _ := param["name"].(string)
					pIn, _ := param["in"].(string)
					pDesc, _ := param["description"].(string)
					pRequired, _ := param["required"].(bool)

					specParam := map[string]any{
						"name":     pName,
						"type":     "string",
						"in":       pIn,
						"required": pRequired,
					}
					if pDesc != "" {
						specParam["description"] = pDesc
					}
					specParams = append(specParams, specParam)
				}
				if len(specParams) > 0 {
					action["params"] = specParams
				}
			}

			actions = append(actions, action)
		}
	}

	if len(actions) > 0 {
		spec["actions"] = actions
	}

	dir := filepath.Join("apis", "developer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	outPath := filepath.Join(dir, name+".yaml")

	data, err := yaml.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshaling spec: %w", err)
	}

	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return fmt.Errorf("writing spec: %w", err)
	}

	fmt.Printf("Created spec from OpenAPI: %s\n", outPath)
	fmt.Printf("  Name: %s\n", name)
	fmt.Printf("  Base URL: %s\n", baseURL)
	fmt.Printf("  Actions: %d\n", len(actions))
	fmt.Println("Edit the file to adjust categories, auth, and transforms.")
	return nil
}

func init() {
	initSpecCmd.Flags().StringVar(&initFromURL, "from", "", "Generate spec from an OpenAPI URL")
	rootCmd.AddCommand(initSpecCmd)
}
