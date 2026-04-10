## CLI Development Rules

### Copyright & Code Comments
- **Every `.go` file must start with the copyright header:**
  ```go
  // Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
  ```
- **When creating a new file,** add the copyright header as the first line, before the package declaration.
- **Add comments to explain the flow,** not what the code does. Focus on:
  - Why a decision was made (not what the code does - the code shows that)
  - How data flows between packages (e.g., "spec resolution order: project > workspace > personal > curated")
  - Non-obvious behavior (e.g., "stdio servers always discover tools at runtime")
  - Section separators for long files using `// ---------------------------------------------------------------------------`
- **Package-level doc comments** (the comment before `package foo`) should explain what the package does and how it fits into the system.
- **Exported functions** should have a one-line doc comment explaining what they do.

### Documentation Accuracy
- **Documentation must always reflect the actual code.** Before documenting a command, flag, or behavior, verify it exists in the source.
- **Never document planned features as if they exist.** If a feature is not implemented, mark it clearly as "Planned" or do not include it.
- **When adding a new command,** update ALL of the following in the same PR:
  - `README.md` - commands table and relevant sections
  - `docs/cli-reference.md` - full command reference
  - `llms.txt` - key commands list
  - `CLAUDE.md` - implemented commands list
- **When removing or renaming a command,** update all documentation files that reference it.
- **Run `grep -r "command_name" docs/ README.md llms.txt` to find all references** before changing any command name.
- To verify which commands actually exist: `grep 'Use:' internal/command/*.go`

### Files That Must Stay in Sync
| File | What it contains | Update when |
|------|-----------------|-------------|
| `README.md` | User-facing CLI docs | Commands, flags, config, features change |
| `docs/cli-reference.md` | Full command reference | Any command, flag, or option changes |
| `docs/security.md` | Credential handling, .env, sandbox | Auth, .env, or transform security changes |
| `docs/memory.md` | Memory system docs | Memory commands or storage changes |
| `docs/spec-format.md` | Tool spec YAML format | Spec parsing, auth, or transform changes |
| `llms.txt` | Machine-readable summary | Any feature, command, or concept changes |
| `CLAUDE.md` | This file | Commands, structure, or patterns change |
| `SECURITY.md` | Security policy | Vulnerability scope or process changes |
| `CONTRIBUTING.md` | Contribution guide | Dev workflow or structure changes |

### Language & Build
- **Go 1.23+** with standard `gofmt` formatting
- Build: `go build ./cmd/clictl/...`
- Test: `go test ./...`
- Lint: `go vet ./...`

### Testing Requirements
- **Run `go test ./...` after every code change** to catch regressions
- **Every new feature must include unit tests** before it is considered complete
- All new exported functions must have unit tests
- Use `t.TempDir()` for file-based tests, `t.Setenv()` for env vars
- Test files live next to source: `foo.go` -> `foo_test.go`
- Use `httptest.NewServer` for HTTP client tests

### Code Style
- Standard `gofmt` formatting
- Explicit `if err != nil` error handling, no panics in production paths
- Context (`ctx`) must be the first argument in long-running or network functions
- Use `fmt.Errorf("doing X: %w", err)` for error wrapping

### Module Path
- Go module: `github.com/clictl/cli`
- All imports use this path

### Implemented Commands
These commands exist in `internal/command/` and are registered:
- `search <query>` - search.go
- `list` - list.go
- `categories` - search.go (list categories with tool counts)
- `tags` - search.go (list popular tags)
- `info <tool>` (alias: inspect) - inspect.go (shows package info, alias resolution, publisher)
- `explain <tool> <action>` - explain.go (structured JSON help for agents)
- `home <tool>` - home.go (open tool website in browser)
- `run <tool> <action>` - run.go (alias: exec, --json skips all transforms and returns raw JSON)
- `install [tool...]` - install.go (--trust for unverified tools, --no-mcp, --no-skill, --target, --workspace, --yes); `install group <name>` installs all tools in a named group
- `uninstall [tool...]` - uninstall.go
- `upgrade [tool...] | --all` - upgrade.go (upgrades installed tools to latest versions, shows version diff, updates lock file)
- `outdated` - outdated.go (show tools with newer versions)
- `tool disable <tool>` - disable.go
- `tool enable <tool>` - disable.go
- `tool pin <tool>` - pin.go
- `tool unpin <tool>` - pin.go
- `tool info <tool>` - inspect.go (alias for `info`)
- `mcp-serve [tools...]` - mcp_serve.go (gateway mode: auto-proxies MCP-protocol specs, --no-sandbox to disable isolation, --code-mode to enable execute_code tool with typed API bindings)
- `mcp list-tools <server>` - mcp.go (shows tool names and parameters)
- `mcp discover <url>` - mcp.go
- `login` - login.go
- `logout` - logout.go
- `whoami` - whoami.go
- `workspace show|switch` - workspace.go
- `permissions` - permissions_cmd.go (check tool access in workspace)
- `request <tool>` - request.go (request tool access)
- `requests` - request.go (list/approve/deny access requests)
- `toolbox add|remove|list|update|validate|create` - toolbox.go
- `publish <spec.yaml>` - publish.go (publish a spec, requires login)
- `update` - update.go (deprecated, use `self-update` for CLI updates)
- `self-update` - self_update.go (update the clictl CLI binary)
- `version` - version.go
- `remember <tool> <note>` - remember.go
- `memory [tool]` - memory.go
- `forget <tool>` - forget.go
- `feedback <tool> <up|down>` - feedback.go
- `transform` - transform.go
- `star <tool>` - star.go
- `unstar <tool>` - star.go
- `stars` - star.go
- `metrics [tool]` - metrics.go
- `uses <tool>` - uses.go (show which installed tools reference a given tool)
- `skill manifest <dir>` - skill_manifest.go (generate YAML file manifest with SHA256 hashes)
- `report <tool> --reason <reason>` - report.go (reports broken/malicious tools, auto-disable at 3+ reports)
- `audit` - audit.go (checks installed tools against registry for issues, verifies against lock file if present)
- `instructions` - instructions.go (show discovery rules to add to CLAUDE.md or AGENTS.md)
- `verify [tool] [--all]` - verify.go (verifies tool integrity against registry and lock file etags, exit code 1 on mismatch)
- `init` - init_spec.go (create a new spec interactively, --from for OpenAPI)
- `test <tool>` - test_spec.go (validate spec against live API)
- `doctor` - doctor.go (diagnose issues)
- `cleanup` - cleanup.go (remove stale cache, orphaned lock entries/memory; --dry-run, --all; auto-runs on install every 30 days)
- `vault set/get/list/delete` - vault_cmd.go (encrypted secret storage, --project for per-project vault)
- `vault export` - vault_cmd.go (export secrets as plaintext, --format env, requires --confirm)
- `vault import <file>` - vault_cmd.go (migrate .env to vault, --exclude to skip keys)
- `vault init` - vault_cmd.go (generate vault key, --force to reset, --password for portable key)
- `toolbox sync` - toolbox.go (push local repo metadata to API, reads .clictl.yaml, --dry-run)
- `team create|list|show|members` - team.go
- `pack <directory>` - pack.go (build skill pack archive for testing)
- `codegen <tool>` - codegen.go (generate typed SDK code from spec, --lang typescript/python, --out file)

### Project Structure
```
cmd/clictl/          # Entry point
internal/
  command/           # Cobra CLI commands
  config/            # Config loading, auth resolution, .env, update config
  executor/          # Protocol-specific execution (HTTP, MCP) with RFC cache + compression
                     #   mcp.go - MCP protocol executor
  httpcache/         # RFC 7234 response cache (bbolt, multi-instance safe)
  logger/            # Structured logging (text/json, file output, level filtering)
  mcp/               # MCP stdio server and client
                     #   client.go - MCP client for consuming upstream MCP servers
                     #   pool.go - Connection pool for MCP client connections
  memory/            # Local tool memory (remember/forget)
  models/            # Data structures
  sandbox/           # OS-level process isolation for MCP servers (env scrubbing, Landlock, sandbox-exec, Job Objects)
  suggest/           # "Did you mean?" fuzzy matching for tool names
  codegen/           # TypeScript and Python SDK code generation from specs
  codemode/          # Sandboxed JS execution with API client bindings for code mode
  transform/         # Response transform pipeline (extract, select, template, js, prefix, only, inject, redact, cost, etc.)
  registry/          # API client, cache, index, spec resolution
                     #   resolve.go - spec resolution from multiple sources
  telemetry/         # Anonymous usage event tracking (queue, batch flush, opt-out)
  vault/             # Encrypted secret storage and vault:// resolution
                     #   vault.go - vault read/write with file locking and atomic writes
                     #   resolve.go - vault:// reference resolution (project -> user -> workspace)
  signing/           # Ed25519 key generation and signature verification
  archive/           # Skill pack archive building, hashing, pack/unpack
  updater/           # Auto-update, version check, registry sync
```

### Key Patterns
- Config precedence: flag > env var > config file > default
- Auth precedence: --api-key > CLICTL_API_KEY > config api_key > config access_token
- Login is NOT required to use clictl. It only provides access to tools you or your organization registered on the platform.
- Default API URL: `https://api.clictl.dev` (only override for dev)
- Config file: `~/.clictl/config.yaml` with 0600 permissions
- Spec cache: `~/.clictl/cache/` with ETag-based validation
- Memory storage: `~/.clictl/memory/` as JSON files, one per tool
- Workspace registry inheritance: when logged in with an active workspace, the CLI fetches the workspace's registry sources via `GET /registries/cli-index/` and merges them with local config. Workspace sources take priority. Cached in `~/.clictl/workspace-cache/<slug>.json` with 5 min TTL.
- Registry resolution order: workspace sources first, then local config registries, then default API
- Favorites: `star`/`unstar`/`stars` commands manage workspace favorites via API. Favorites boost search ranking.
- Metrics: `metrics` command shows workspace usage stats. `metrics <tool>` shows per-tool detail. `--days` flag (default 30).
- Log config: off by default, configurable level/format/file via yaml or env vars
- Response cache: bbolt-backed, RFC 7234, off by default, configurable size
- Auto-sync: weekly registry index sync, weekly version check (configurable)
- HTTP requests use Accept-Encoding: gzip, deflate for compression
- Version set via ldflags at build: `-X github.com/clictl/cli/internal/command.Version=v1.0.0`
- .env files loaded from: cwd, project root (.git parent), ~/.clictl/.env
- JS transforms sandboxed: no fetch, eval, require, setTimeout, or constructor access
- Generated skill files include memories from ~/.clictl/memory/
- Vault storage: `~/.clictl/vault.enc` (encrypted) + `~/.clictl/vault.key` (32 random bytes, 0600 perms)
- Project vault: `.clictl/vault.enc` in git root (auto-added to .gitignore)
- vault:// resolution order: project vault, user vault, workspace vault (enterprise), raw env
- Telemetry: on by default, opt out via `telemetry: false` in config.yaml. Fire-and-forget, batched every 5 min, 1000-event queue cap
- Toolbox sync: `clictl toolbox sync` reads `.clictl.yaml` (workspace, namespace, spec_paths) and POSTs index entries to API

## Spec Format 1.0

All tool specs use `spec: "1.0"` format. Key rules:

### Protocol Types
- `http` - REST API tools
- `mcp` - MCP server tools
- `skill` - Claude Code skill files
- `website` - Web-based tools
- `command` - CLI command tools

### Auth Format
Auth uses `auth.env` to name the environment variable and `auth.header` with a template string for injection:
```yaml
auth:
  env: MY_API_KEY
  header: "Authorization: Bearer ${MY_API_KEY}"
```
Or `auth.param` for query parameter injection. No other auth fields exist. Never use `auth.value`, `auth.inject`, `auth.type`, `auth.headers`, or `auth.scopes`.

### MCP Servers
- `actions` in MCP specs are metadata for search/discovery only. Actual tools are discovered at runtime via the MCP protocol.
- There is no `tools.expose` field. All tools from an MCP server are available.
- There is no `discover` field. stdio servers always discover tools automatically.
- `clictl_code` management tool is available in MCP server mode.

### Spec Field Rules
- Use `spec: "1.0"` at the top level. There is no `schema_version` field.
- `--json` flag on `clictl run` skips all transforms and returns raw JSON.

### Anti-Patterns
- **No old auth format:** Never use `auth.value`, `auth.inject`, `auth.type`, `auth.headers`, or `auth.scopes`. Use `auth.env` + `auth.header` with a template string instead.
- **No `schema_version`:** Use `spec: "1.0"` instead.
- **No `discover` field:** stdio MCP servers always discover tools automatically.
- **No `tools.expose`:** All tools from an MCP server are available at runtime.
