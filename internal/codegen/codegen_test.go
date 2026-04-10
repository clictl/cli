// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package codegen

import (
	"strings"
	"testing"

	"github.com/clictl/cli/internal/models"
)

func testSpec() *models.ToolSpec {
	return &models.ToolSpec{
		Name:        "acme-api",
		Version:     "2.0",
		Description: "Acme platform API",
		Server: &models.Server{
			Type: "http",
			URL:  "https://api.acme.com/v2",
		},
		Auth: &models.Auth{
			Env:    models.StringOrSlice{"ACME_API_KEY"},
			Header: "Authorization: Bearer ${ACME_API_KEY}",
		},
		Actions: []models.Action{
			{
				Name:        "list-users",
				Description: "List all users",
				Method:      "GET",
				URL:         "https://api.acme.com/v2",
				Path:        "/users",
				Params: []models.Param{
					{Name: "page", Type: "integer", Description: "Page number"},
					{Name: "per_page", Type: "integer", Default: "25", Description: "Results per page"},
				},
			},
			{
				Name:        "create-user",
				Description: "Create a new user",
				Method:      "POST",
				URL:         "https://api.acme.com/v2",
				Path:        "/users",
				Mutable:     true,
				Params: []models.Param{
					{Name: "email", Type: "string", Required: true, Description: "User email"},
					{Name: "name", Type: "string", Description: "Full name"},
					{Name: "role", Type: "string", Values: []string{"admin", "member", "viewer"}, Description: "User role"},
				},
			},
			{
				Name:        "list-invoices",
				Description: "List billing invoices",
				URL:         "https://billing.acme.com/v1",
				Path:        "/invoices",
				Auth: &models.Auth{
					Env:    models.StringOrSlice{"ACME_BILLING_KEY"},
					Header: "Authorization: Bearer ${ACME_BILLING_KEY}",
				},
				Params: []models.Param{
					{Name: "status", Type: "string", Values: []string{"draft", "open", "paid", "void"}, Description: "Filter by status"},
				},
			},
			{
				Name:        "health",
				Description: "Health check endpoint",
				URL:         "https://api.acme.com",
				Path:        "/health",
			},
		},
	}
}

func TestToCamelCase(t *testing.T) {
	tests := []struct{ in, want string }{
		{"list-repos", "listRepos"},
		{"get_user", "getUser"},
		{"health", "health"},
		{"create-webhook-endpoint", "createWebhookEndpoint"},
		{"listRepos", "listRepos"},
	}
	for _, tt := range tests {
		got := ToCamelCase(tt.in)
		if got != tt.want {
			t.Errorf("ToCamelCase(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestToPascalCase(t *testing.T) {
	tests := []struct{ in, want string }{
		{"list-repos", "ListRepos"},
		{"get_user", "GetUser"},
		{"health", "Health"},
	}
	for _, tt := range tests {
		got := ToPascalCase(tt.in)
		if got != tt.want {
			t.Errorf("ToPascalCase(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct{ in, want string }{
		{"list-repos", "list_repos"},
		{"health", "health"},
		{"create-webhook-endpoint", "create_webhook_endpoint"},
	}
	for _, tt := range tests {
		got := ToSnakeCase(tt.in)
		if got != tt.want {
			t.Errorf("ToSnakeCase(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestGenerateTypeScript(t *testing.T) {
	spec := testSpec()
	out := GenerateTypeScript(spec)

	// Check header
	if !strings.Contains(out, "// Tool: acme-api v2.0") {
		t.Error("missing tool header")
	}

	// Check env vars
	if !strings.Contains(out, "ACME_API_KEY") {
		t.Error("missing ACME_API_KEY env var")
	}
	if !strings.Contains(out, "ACME_BILLING_KEY") {
		t.Error("missing ACME_BILLING_KEY env var")
	}

	// Check param interface
	if !strings.Contains(out, "export interface ListUsersParams") {
		t.Error("missing ListUsersParams interface")
	}
	if !strings.Contains(out, "page?: number") {
		t.Error("missing page param with number type")
	}

	// Check required vs optional
	if !strings.Contains(out, "email: string") {
		t.Error("required param should not have ?")
	}
	if !strings.Contains(out, "name?: string") {
		t.Error("optional param should have ?")
	}

	// Check enum values
	if !strings.Contains(out, `"admin" | "member" | "viewer"`) {
		t.Error("missing enum union type for role")
	}

	// Check function declarations
	if !strings.Contains(out, "export declare function listUsers(params: ListUsersParams): Promise<any>") {
		t.Error("missing listUsers function declaration")
	}
	if !strings.Contains(out, "export declare function health(): Promise<any>") {
		t.Error("missing no-param health function")
	}

	// Check multi-URL
	if !strings.Contains(out, "GET https://billing.acme.com/v1/invoices") {
		t.Error("missing billing URL in comment")
	}

	// Check JSDoc
	if !strings.Contains(out, "/** List all users */") {
		t.Error("missing JSDoc comment")
	}
}

func TestGeneratePython(t *testing.T) {
	spec := testSpec()
	out := GeneratePython(spec)

	// Check header
	if !strings.Contains(out, "Tool: acme-api v2.0") {
		t.Error("missing tool header")
	}

	// Check imports
	if !strings.Contains(out, "from dataclasses import dataclass") {
		t.Error("missing dataclass import")
	}
	if !strings.Contains(out, "Literal") {
		t.Error("missing Literal import for enum values")
	}

	// Check dataclass
	if !strings.Contains(out, "class ListUsersParams:") {
		t.Error("missing ListUsersParams class")
	}
	if !strings.Contains(out, "page: Optional[int] = None") {
		t.Error("missing optional int param")
	}

	// Check required first
	if !strings.Contains(out, "email: str\n") {
		t.Error("missing required str param")
	}

	// Check enum Literal
	if !strings.Contains(out, `Literal["admin", "member", "viewer"]`) {
		t.Error("missing Literal enum type")
	}

	// Check function
	if !strings.Contains(out, "async def list_users(params: ListUsersParams) -> Any:") {
		t.Error("missing list_users function")
	}
	if !strings.Contains(out, "async def health() -> Any:") {
		t.Error("missing no-param health function")
	}
}

func TestGenerateTypeScriptDeclarations(t *testing.T) {
	spec := testSpec()
	out := GenerateTypeScriptDeclarations([]*models.ToolSpec{spec})

	if !strings.Contains(out, "declare const acmeApi:") {
		t.Error("missing namespace declaration")
	}
	if !strings.Contains(out, "listUsers(") {
		t.Error("missing listUsers inline declaration")
	}
	if !strings.Contains(out, "email: string") {
		t.Error("missing required param in inline decl")
	}
}
