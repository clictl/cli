# CLI Reference

Complete reference for all clictl commands, flags, and configuration.

## Global Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--output` | `-o` | Output format: `text`, `json`, `yaml` |
| `--api-url` | | Override the registry API URL |
| `--api-key` | | API key for authentication |
| `--no-cache` | | Bypass local spec cache |
| `--help` | `-h` | Show help for any command |
| `--version` | `-v` | Print CLI version |

## Discovery

### search

Find tools in the registry by keyword.

```bash
clictl search weather
clictl search "github api"
clictl search openai --output json
```

Filter results:

```bash
clictl search api --category developer     # by category
clictl search docker --type cli            # by type (api, cli, website)
clictl search translation --tag ai         # by tag
clictl search weather --auth none          # by auth type (none, api_key, bearer, oauth2, any)
clictl search tools --ready                # only tools ready to use (no auth required)
clictl search weather --protocol mcp       # filter by protocol (rest, mcp, graphql, etc.)
```

Searches tool names, descriptions, tags, and categories. Returns results ranked by relevance.

### list

Browse all tools, optionally filtered.

```bash
clictl list
clictl list --category ai
clictl list --protocol mcp                    # filter by protocol
clictl list --category developer-tools --output json
```

### categories

List all available categories with tool counts.

```bash
clictl categories
clictl categories --output json
```

### tags

List popular tags across the registry.

```bash
clictl tags
clictl tags --output json
```

### info (alias: inspect)

Show detailed info about a tool: actions, parameters, auth requirements.

```bash
clictl info open-meteo
clictl info github --output json
```

When a tool has memories attached, they appear at the end of the info output.

**Alias resolution:** If the tool was resolved from an alias, a note is printed to stderr showing the original and resolved name.

**Package info:** If the tool has package metadata (registry, package name, version, SHA256), a "Package" section is shown in the output.

### explain

Show structured JSON help for a specific tool action. Designed for agents that need parameter details programmatically.

```bash
clictl explain open-meteo current
clictl explain github repos
```

### home

Open a tool's website or documentation in the default browser.

```bash
clictl home open-meteo
clictl home github
```

## Execution

### run (alias: exec)

Run a tool action with parameters. Works transparently with REST, CLI, and MCP-protocol specs.

```bash
clictl run open-meteo current --latitude 51.5 --longitude -0.12
clictl run github repos --username rickcrawford
clictl run slack post-message --channel general --text "Hello"
```

**MCP server tools** work the same way. If the spec uses an MCP server type, clictl connects to the MCP server and invokes the tool:

```bash
clictl run github-mcp search_repositories --query "clictl"
```

Parameters are passed as `--name value` flags. Required parameters that are missing will produce an error with the expected format.

**Output formats:**

```bash
clictl run github repos --username rick --output json
clictl run github repos --username rick --output yaml
clictl run github repos --username rick | jq '.[] .name'
```

## Installation

### install

Install a tool as an agent skill and register an MCP server. Both are created by default.

```bash
clictl install open-meteo                    # skill + MCP server (default)
clictl install open-meteo --no-mcp           # skill only, skip MCP registration
clictl install open-meteo --no-skill         # MCP only, skip skill file
clictl install open-meteo --target cursor
clictl install open-meteo --target claude-desktop
clictl install group hacker-news             # install all tools in a group
```

Creates `.claude/skills/<tool>/SKILL.md` and adds an entry to `.mcp.json` (or the target's config file). Use `--no-mcp` to skip MCP registration or `--no-skill` to skip the skill file.

**Skill protocol specs:** For skill-protocol tools, `install` fetches the SKILL.md and any additional files listed in `source.files` rather than generating them from the spec. This provides richer documentation authored by the tool maintainer.

```bash
clictl install github-mcp                   # fetches SKILL.md + supporting files from source
```

**SHA256 integrity verification:** When a skill spec includes `sha256` hashes in `source.files`, the CLI verifies each downloaded file against the expected hash. If a hash does not match, the install is aborted and an error is shown. If a file has no hash, a warning is printed but the install continues.

**Unverified publisher warning:** Tools without a verified publisher (no namespace or verification status) display a warning and are skipped by default. Use `--trust` to install unverified tools:

```bash
clictl install community-tool --trust       # install despite unverified publisher
```

**Group install:** Install all tools in a named group at once. The group manifest is fetched from the registry and each member tool is installed.

```bash
clictl install group hacker-news            # install all tools in the hacker-news group
clictl install group data-science --trust   # install group with unverified tools
```

**Alias resolution:** If a tool name was resolved from an alias (e.g., an old name), a note is printed showing the resolution path.

| Flag | Description |
|------|-------------|
| `--no-mcp` | Skip MCP server registration (skill file only) |
| `--no-skill` | Skip skill file (MCP only) |
| `--target` | Target AI tool: claude-code, gemini, codex, cursor, windsurf (auto-detected if omitted) |
| `--workspace` | Sync installed tools to the active workspace |
| `--yes` | Skip permission confirmation prompts |
| `--trust` | Install tools from unverified publishers |

### uninstall

Remove a tool's skill file and MCP registration.

```bash
clictl uninstall open-meteo
```

### upgrade

Update installed tools to the latest spec version. Shows version diffs when available and updates the lock file after successful upgrades.

```bash
clictl upgrade github stripe             # upgrade specific tools
clictl upgrade --all                     # upgrade all installed tools
clictl upgrade --all --yes               # skip confirmation prompts
```

| Flag | Description |
|------|-------------|
| `--all` | Upgrade all installed tools |
| `--yes` | Skip confirmation prompts |
| `--target` | Target AI tool: claude-code, gemini, codex, cursor, windsurf |

### mcp-serve

Run as an MCP stdio server, exposing tool actions as MCP tools.

```bash
clictl mcp-serve open-meteo github           # serve specific tools
clictl mcp-serve                                  # serve all installed tools
```

Uses JSON-RPC over stdio. Each tool action becomes an MCP tool with proper JSON Schema for parameters.

**Gateway mode:** MCP server specs are auto-proxied alongside regular tool actions. When a tool uses an MCP server type, clictl connects to that upstream MCP server and exposes its tools through the clictl MCP server. Your AI client sees a unified set of tools regardless of the underlying server type.

| Flag | Description |
|------|-------------|
| `--tools-only` | Serve only specified tools, no clictl management commands |
| `--no-sandbox` | Disable process sandboxing for MCP servers |
| `--code-mode` | Add `execute_code` tool with typed API bindings for agent code execution |

**Sandbox:** MCP server subprocesses are sandboxed by default. Environment variables are scrubbed to an allowlist (only declared vars pass through). On Linux, Landlock restricts filesystem access. On macOS, sandbox-exec applies restrictions. Use `--no-sandbox` to disable, or set `sandbox: false` in `~/.clictl/config.yaml`.

**Code mode:** When `--code-mode` is enabled, the MCP server exposes an `execute_code` tool alongside regular tools. The tool description includes TypeScript type definitions for all loaded specs. Agents write JavaScript code that calls typed API methods. Code executes in a sandboxed runtime with a 30-second timeout. All HTTP calls route through the Go executor with SSRF protection and auth injection.

### codegen

Generate typed SDK code from a tool spec. Produces TypeScript or Python with typed interfaces for all action parameters and function declarations.

```bash
clictl codegen github --lang typescript          # TypeScript to stdout
clictl codegen stripe --lang python --out sdk.py # Python to file
clictl codegen --all --lang typescript --out ./sdk/  # all installed tools
```

| Flag | Description |
|------|-------------|
| `--lang` | Output language: `typescript` (default), `python` |
| `--out` | Output file or directory (default: stdout) |
| `--all` | Generate for all installed tools |

### skill manifest

Generate a YAML file manifest with SHA256 hashes from a local directory. Designed for skill authors who need to produce the `source.files` section of their spec.

```bash
clictl skill manifest ./my-skill/
```

Scans the directory and outputs a `files:` array to stdout with relative paths and SHA256 hashes for each file:

```yaml
files:
  - path: SKILL.md
    sha256: a1b2c3d4e5f6...
  - path: scripts/setup.sh
    sha256: f6e5d4c3b2a1...
  - path: templates/config.yaml
    sha256: 1a2b3c4d5e6f...
```

Copy this output into the `skill.source` section of your tool spec. The registry uses these hashes to verify file integrity during `clictl install`.

### mcp list-tools

List available tools from an MCP server spec.

```bash
clictl mcp list-tools github-mcp
clictl mcp list-tools github-mcp --output json
```

Shows the tool names, descriptions, and parameter schemas exposed by the MCP server defined in the tool spec.

**tools.mode:** MCP specs support a `tools.mode` field that controls which tools are exposed. In `explicit` mode (the default), only tools listed in the spec's `tools` section are exposed. In `dynamic` mode, all tools reported by the MCP server are passed through.

### mcp discover

Discover tools from an ad-hoc HTTP MCP server. Useful for exploring MCP servers that are not yet in the registry.

```bash
clictl mcp discover https://mcp.example.com
clictl mcp discover https://mcp.example.com --output json
clictl mcp discover https://mcp.example.com --generate-spec    # generate a clictl spec from discovered tools
```

| Flag | Description |
|------|-------------|
| `--generate-spec` | Generate a clictl YAML spec from the discovered tools |

## Memory

### remember

Attach a note to a tool. Memories appear on inspect and in skill files.

```bash
clictl remember open-meteo "use --units metric for EU"
clictl remember github "rate limit is 5000/hr with token"
```

### memory

Show all memories for a tool.

```bash
clictl memory open-meteo
clictl memory --all                               # list all tools with memories
```

### forget

Remove memories.

```bash
clictl forget open-meteo                      # interactive: pick which to remove
clictl forget open-meteo --all                # remove all memories for a tool
```

## Transforms

### transform

Test transform pipelines on JSON data from stdin. Useful for developing transforms before adding them to a tool spec.

**From flags:**

```bash
echo '{"data": [{"name": "a"}, {"name": "b"}]}' | clictl transform --extract '$.data'
echo '{"items": [{"n": "a", "x": 1}]}' | clictl transform --extract '$.items' --select 'n' --rename 'n=name'
clictl run my-tool get-data --raw | clictl transform --extract '$.results' --truncate 5
echo '{"html": "<h1>Hi</h1>"}' | clictl transform --extract '$.html' --html-to-markdown
```

**From a YAML file:**

```bash
echo '{"data": [1,2,3]}' | clictl transform --file transforms.yaml
```

The YAML file contains a list of transform steps:

```yaml
- type: json
  extract: "$.data.items"
  select: ["name", "status"]
- type: truncate
  max_items: 10
- type: template
  body: |
    {{range .}}- {{.name}}: {{.status}}
    {{end}}
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--extract` | JSONPath expression (e.g., `$.data.items`) |
| `--select` | Fields to keep (comma-separated) |
| `--rename` | Rename fields (e.g., `dt=date,temp_max=high`) |
| `--truncate` | Max array items to keep |
| `--template` | Go template string |
| `--html-to-markdown` | Convert HTML to markdown |
| `--file` | YAML file with a full transform pipeline |

**Full pipeline:**

```
pre_transform -> HTTP request -> assert -> transform -> Output
```

**Transform types reference:**

| Stage | Type | Input | Output | Purpose |
|-------|------|-------|--------|---------|
| pre_transform | `default_params` | params | params | Inject defaults for missing params |
| pre_transform | `rename_params` | params | params | Rename params before sending |
| pre_transform | `template_body` | params | body string | Build request body from template |
| pre_transform | `js` | {params, body} | {params, body} | Custom pre-processing |
| assert | `status` | HTTP status | pass/fail | Check response status code |
| assert | `exists` | JSON response | pass/fail | JSONPath field must exist |
| assert | `not_empty` | JSON response | pass/fail | Field must not be empty |
| assert | `equals` | JSON response | pass/fail | Field must equal value |
| assert | `contains` | response body | pass/fail | Body must contain string |
| assert | `js` | {status_code, body} | {pass, reason} | Custom validation |
| transform | `extract` | JSON | JSON subset | Pull data from nested response |
| transform | `select` | object/array | fewer fields | Keep only named fields |
| transform | `rename` | object/array | renamed fields | Rename fields for clarity |
| transform | `truncate` | array/string | smaller | Limit size |
| transform | `template` | any | string | Format for display |
| transform | `html_to_markdown` | HTML string | markdown | Convert web content |
| transform | `js` | any | any | Custom transformation |
| transform | `prefix` | string | string | Add a prefix to string output |
| transform | `only` | object/array | filtered | Keep items matching a condition |
| transform | `inject` | any | any | Inject static data into the output |
| transform | `redact` | any | any | Remove sensitive fields from output |
| transform | `cost` | any | any | Calculate and annotate cost metadata |

## Authentication

### login

Authenticate with the clictl platform.

```bash
clictl login                                      # browser OAuth (recommended)
clictl login --api-key CLAK-...                   # API key
```

Browser OAuth opens your default browser, completes via a local callback server, and stores tokens automatically.

### logout

Clear stored credentials.

```bash
clictl logout
```

### whoami

Show the current authenticated user.

```bash
clictl whoami
```

**Token resolution order:** `--api-key` flag > `CLICTL_API_KEY` env var > config `api_key` > config `access_token`

## Tool Connections

### connect

Initiate an OAuth connection to a tool that requires user authorization. Opens your browser to authorize access. The connection is stored on the platform and used automatically when you `run` the tool.

```bash
clictl connect slack
clictl connect spotify
```

Requires login. If the tool does not use OAuth, displays the auth type and manual setup instructions.

### star

Favorite a tool. Favorites are synced to your workspace and boost search ranking.

```bash
clictl star open-meteo
```

Requires login.

### unstar

Remove a tool from your favorites.

```bash
clictl unstar open-meteo
```

Requires login.

### stars

List your favorited tools.

```bash
clictl stars
clictl stars --output json
```

Requires login.

### metrics

Show workspace usage statistics. Without a tool name, shows aggregate stats. With a tool name, shows per-tool detail.

```bash
clictl metrics                   # workspace-level stats
clictl metrics open-meteo        # per-tool stats
clictl metrics --days 90         # custom time window (default: 30)
```

| Flag | Description |
|------|-------------|
| `--days` | Number of days to show stats for (default: 30) |

Requires login and an active workspace.

### uses

Show which installed tools and skill files reference a given tool. Useful for understanding dependencies.

```bash
clictl uses open-meteo
```

### feedback

Submit feedback on a tool. Ratings go to the tool maintainer.

```bash
clictl feedback open-meteo up
clictl feedback open-meteo down --label outdated
clictl feedback slack up --label accurate --comment "Great API coverage"
```

| Flag | Description |
|------|-------------|
| `--label` | Label: `accurate`, `helpful`, `outdated`, `inaccurate`, `incomplete` |
| `--comment` | Optional comment for the maintainer |

Requires login.

## Tool Management

### tool pin

Pin a tool to its current version. Pinned tools are skipped by `upgrade`.

```bash
clictl tool pin open-meteo
```

### tool unpin

Remove the version pin from a tool, allowing upgrades again.

```bash
clictl tool unpin open-meteo
```

### tool disable

Disable a tool so it cannot be executed. Persisted in `~/.clictl/config.yaml`. The tool remains installed but `run` will refuse to execute it.

```bash
clictl tool disable terraform-cli
```

### tool enable

Re-enable a previously disabled tool.

```bash
clictl tool enable terraform-cli
```

### tool info

Show detailed info about a tool (alias for `clictl info`).

```bash
clictl tool info open-meteo
```

## Workspace & Permissions

### workspace switch

Set the active workspace. When a workspace is active, tool execution is gated by the workspace's permission policy. Running without arguments shows an interactive picker.

```bash
clictl workspace switch                          # interactive picker
clictl workspace switch my-team                  # switch directly by slug
clictl workspace switch ""                       # clear active workspace
```

### workspace show

Show the current active workspace.

```bash
clictl workspace show
```

### permissions

Check which tools and actions you have access to in the active workspace.

```bash
clictl permissions
clictl permissions --tool github
clictl permissions --output json
```

| Flag | Description |
|------|-------------|
| `--tool` | Check a specific tool (default: all) |

Requires login and an active workspace.

### request

Request access to a tool in the active workspace. An admin can approve or deny it.

```bash
clictl request slack
clictl request slack --reason "Need for customer support integration"
```

| Flag | Description |
|------|-------------|
| `--reason` | Reason for requesting access |

Requires login and an active workspace.

### requests

List tool access requests for the active workspace.

```bash
clictl requests
clictl requests --status pending
clictl requests --output json
```

| Flag | Description |
|------|-------------|
| `--status` | Filter by status: `pending`, `approved`, `denied` |

### requests approve

Approve a tool access request.

```bash
clictl requests approve 123
clictl requests approve 123 --note "Approved for Q2 project"
```

### requests deny

Deny a tool access request.

```bash
clictl requests deny 456
clictl requests deny 456 --note "Use the internal API instead"
```

## Teams

### team create

Create a team in the active workspace.

```bash
clictl team create my-team
```

### team list

List all teams in the active workspace.

```bash
clictl team list
```

### team show

Show details about a team.

```bash
clictl team show my-team
```

### team members

List members of a team.

```bash
clictl team members my-team
```

## Publishing

### publish

Publish a tool spec to the registry. Requires login.

```bash
clictl publish my-tool.yaml
clictl publish specs/o/open-meteo.yaml
```

## Authoring

### init

Create a new tool spec interactively. Can also generate a spec from an OpenAPI definition.

```bash
clictl init                                       # interactive spec creation
clictl init --from https://api.example.com/openapi.json  # generate from OpenAPI
```

### test

Validate a tool spec against the live API. Runs each action and checks assertions.

```bash
clictl test open-meteo
clictl test my-tool --action get-data
```

### pack

Build a skill pack archive from a local directory. Useful for testing packs before publishing via the platform.

```bash
clictl pack ./my-skill                            # build archive in current directory
clictl pack ./my-skill --output dist/             # specify output directory
```

The pack command:
1. Scans the directory for skill files (SKILL.md, scripts, references)
2. Computes content hashes for every file
3. Generates a manifest with file hashes and metadata
4. Creates a `.tar.gz` archive

Local packs are unsigned. To distribute signed packs, publish through the platform.

| Flag | Description |
|------|-------------|
| `--output` | Output directory for the archive (default: current directory) |

## MCP Gateway

### mcp

Run as an MCP gateway server, giving AI agents access to the clictl registry. Agents can search, install, and run tools dynamically.

```bash
clictl mcp                                        # full registry access
clictl mcp anthropic/xlsx github-mcp              # locked to specific tools
```

In full mode, the gateway exposes three tools: `search`, `install`, and `run`. In locked mode (tools specified), only `run` is exposed for the listed tools.

Register in your MCP config:

```json
{
  "mcpServers": {
    "clictl": {
      "command": "clictl",
      "args": ["mcp"]
    }
  }
}
```

## Safety

### report

Report a tool as broken, malicious, or otherwise problematic. Reports are sent to the registry. Tools that receive 3 or more reports are automatically disabled.

```bash
clictl report some-tool --reason "Returns malicious output"
clictl report broken-api --reason "API endpoint no longer exists"
```

| Flag | Description |
|------|-------------|
| `--reason` | Required. Why you are reporting this tool |

Requires login.

### verify

Verify that installed tools match the registry. For signed packs, verifies the registry signature, transparency log entry (when present), and content hashes. For unsigned tools, compares ETags against the local cache and lock file.

```bash
clictl verify github-mcp              # verify a single tool
clictl verify --all                    # verify all installed tools
```

| Flag | Description |
|------|-------------|
| `--all` | Verify all installed tools |

Verification checks (in order):
1. Registry signature against the embedded public key
2. Transparency log entry, if present (certificate-based verification against the public audit log)
3. Archive content hash against the manifest's content_sha256
4. Per-file hashes against the manifest entries
5. ETag comparison against the lock file

Exit code 0 if all tools verified, 1 if any mismatches detected. Output format for signed packs with a transparency log entry:

```
$ clictl verify gstack-ship
  Registry signature: valid
  Transparency log:   valid (logged 2026-03-30, entry #12345)
  Content hash:       valid
  All checks passed.
```

Output format when verifying multiple tools:

```
github-mcp  v1.2.0  verified (signature + transparency log + content hash)
time-mcp    v1.0.0  WARNING: content mismatch (local: sha256:abc1, registry: sha256:def4)
open-meteo  v1.1.0  verified (registry etag matches)
```

Tools without a transparency log entry show "signature + content hash" instead. The transparency log check is additive. It does not block verification when absent.

### audit

Check installed tools against the registry for issues. Verifies lock file integrity, checks for known-blocked tools, and flags tools with no publisher verification.

```bash
clictl audit
```

## Toolboxes

Toolboxes are Git-based tool repositories. Add any Git repo containing tool specs as a toolbox.

### `clictl toolbox add <owner/repo>`

Add a local or workspace toolbox.

| Flag | Description |
|------|-------------|
| `--sync-mode` | Sync mode: full or metadata |
| `--visibility` | Visibility: public, workspace, restricted |
| `--branch` | Default branch to sync from |

### `clictl toolbox remove <name>`

Remove a toolbox by name.

### `clictl toolbox list`

List all configured toolboxes (workspace and local merged).

### `clictl toolbox update [name]`

Sync toolboxes from their sources. Without arguments, syncs all toolboxes.

| Flag | Description |
|------|-------------|
| `--all` | Sync all toolboxes |
| `--trigger` | Trigger server-side sync (for CI) |

### `clictl toolbox validate <path>`

Validate all YAML specs in a directory. Returns exit code 1 if any specs are invalid.

### `clictl toolbox create <name>`

Scaffold a new toolbox directory with `.meta.yaml`, example spec, README, and `.gitignore`.

## System

### cleanup

Remove stale cache, orphaned lock entries, memories, and skill files.

```bash
clictl cleanup
clictl cleanup --dry-run
clictl cleanup --all
```

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be removed without deleting |
| `--all` | Also clear spec cache and etags |

### doctor

Diagnose common issues with your clictl installation: config, auth, registries, dependencies.

```bash
clictl doctor
```

### instructions

Show discovery rules to add to your CLAUDE.md or AGENTS.md file.

```bash
clictl instructions
```

## Updates

### self-update

Update the clictl CLI binary to the latest version. Checks GitHub releases for a newer version and replaces the binary in place. Uses the updater package internally.

```bash
clictl self-update
clictl self-update --skip-verify                 # skip signature verification
```

### update (deprecated)

Sync all configured registries and check for tool updates. Use `self-update` to update the CLI binary.

```bash
clictl update
clictl update --enable-auto                       # enable automatic updates
clictl update --disable-auto                      # disable automatic updates
```

Auto-update checks weekly for registry index updates.

## Vault

Encrypted local secret storage. Secrets are stored in `~/.clictl/vault.enc` and resolved automatically when tools need credentials.

### vault init

Generate the vault encryption key. Run once before using other vault commands.

```bash
clictl vault init
clictl vault init --force                         # regenerate key (destroys existing vault)
clictl vault init --password                      # derive key from password (portable)
```

### vault set

Store a secret in the vault.

```bash
clictl vault set GITHUB_TOKEN ghp_abc123
clictl vault set STRIPE_KEY sk_test_xyz --project  # store in project vault
```

| Flag | Description |
|------|-------------|
| `--project` | Store in per-project vault (`.clictl/vault.enc` in git root) |

### vault get

Retrieve a secret from the vault.

```bash
clictl vault get GITHUB_TOKEN
```

### vault list

List all keys in the vault (values are not shown).

```bash
clictl vault list
```

### vault delete

Remove a secret from the vault.

```bash
clictl vault delete GITHUB_TOKEN
```

### vault export

Export all secrets as plaintext. Requires confirmation.

```bash
clictl vault export --confirm                     # prints key=value pairs
clictl vault export --confirm --format env        # .env file format
```

| Flag | Description |
|------|-------------|
| `--confirm` | Required. Confirms you want to export plaintext secrets |
| `--format` | Output format: `env` (default) |

### vault import

Import secrets from a .env file into the vault.

```bash
clictl vault import .env
clictl vault import .env --exclude AWS_*          # skip matching keys
```

| Flag | Description |
|------|-------------|
| `--exclude` | Glob pattern for keys to skip |

**Resolution order**: When a tool needs a credential, the CLI checks: project vault, user vault, workspace vault (enterprise), then raw environment variable.

## Configuration

Config file: `~/.clictl/config.yaml`

```yaml
api_url: https://api.clictl.dev
output: text
cache_dir: ~/.clictl/cache
auth:
  api_key: ""
  access_token: ""
  refresh_token: ""
registries:
  - name: clictl-official
    type: api
    url: https://api.clictl.dev
    default: true
update:
  auto_update: false
  sync_interval: "168h"
  version_check_interval: "168h"
```

## Piping and Scripting

clictl works well with standard Unix tools:

```bash
# Search and pipe to jq
clictl search weather --output json | jq '.[].name'

# Execute and extract a field
clictl run open-meteo current --latitude 51.5 --longitude -0.12 --output json

# List all tool names
clictl list --output json | jq -r '.[].name'

# Batch info
clictl list --output json | jq -r '.[].name' | xargs -I{} clictl info {}
```

---

**See also:** [Spec Format](spec-format.md) | [Security](security.md) | [Securing Secrets](securing-secrets.md) | [Memory](memory.md)
