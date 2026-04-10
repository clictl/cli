# MCP Integration

clictl works as both an MCP server and an MCP client. Install a tool and it registers as an MCP server automatically. Or consume existing MCP servers transparently through clictl.

## Serving Tools via MCP

Every installed tool is available as an MCP server with no extra setup:

```bash
# Serve all installed tools + management commands
clictl mcp-serve

# Serve specific tools only
clictl mcp-serve github stripe open-meteo

# Serve specific tools, no management commands
clictl mcp-serve github stripe --tools-only

# Enable code mode (adds execute_code tool)
clictl mcp-serve github stripe --code-mode
```

### MCP config for Claude Code

```json
{
  "mcpServers": {
    "clictl": {
      "command": "clictl",
      "args": ["mcp-serve"]
    }
  }
}
```

### MCP config for specific tools

```json
{
  "mcpServers": {
    "clictl": {
      "command": "clictl",
      "args": ["mcp-serve", "github", "stripe", "--tools-only"]
    }
  }
}
```

### What gets exposed

Each tool action becomes an MCP tool with:
- Name: `toolname_actionname` (e.g., `github_user`, `stripe_list-charges`)
- Description from the spec
- JSON Schema for parameters (types, required fields, defaults)

In gateway mode (default, without `--tools-only`), management commands are also exposed:
- `clictl_search` - search the registry
- `clictl_list` - list tools by category
- `clictl_inspect` - get tool details
- `clictl_install` - install a tool
- `clictl_run` - execute any tool action

### Flags

| Flag | Description |
|------|-------------|
| `--tools-only` | Only serve specified tools, no management commands |
| `--no-sandbox` | Disable process sandboxing for MCP server subprocesses |
| `--code-mode` | Add `execute_code` tool with typed API bindings |

## Consuming MCP Servers

clictl can also act as an MCP client. MCP protocol specs (with `package` block or `server.command`) connect to upstream MCP servers transparently.

### Run tools from MCP servers

```bash
clictl run filesystem read_file --path ./README.md
clictl run github-mcp search_repositories --query "clictl"
```

If a tool spec uses an MCP server type, clictl spawns the server, connects via MCP protocol, and invokes the tool. From your perspective, it works exactly like any other tool.

### List tools from an MCP server

```bash
clictl mcp list-tools filesystem
clictl mcp list-tools github-mcp
```

### Discover tools from an HTTP MCP server

```bash
clictl mcp discover https://mcp.example.com
clictl mcp discover https://mcp.example.com --generate-spec
```

The `--generate-spec` flag creates a clictl spec file from the discovered tools, so you can install and customize it.

## Gateway Mode

When running `clictl mcp-serve`, MCP server specs are auto-proxied alongside regular REST tool actions. Your AI client sees a unified set of tools regardless of whether the underlying tool is a REST API, website, or MCP server.

```bash
# This serves REST tools AND proxies MCP server tools together
clictl mcp-serve github filesystem slack
```

The agent doesn't need to know whether `github` is a REST API spec and `filesystem` is an MCP server. Both appear as MCP tools.

## Process Sandboxing

MCP server subprocesses are sandboxed by default:

- **Environment scrubbing** - only declared env vars pass through to the subprocess
- **Filesystem isolation** - sensitive directories (~/.ssh, ~/.aws, browser profiles) are blocked
- **Network restrictions** - platform-specific (Landlock on Linux, sandbox-exec on macOS)
- **Fail-closed** - if sandbox setup fails, the process is not started (configurable via `strict_sandbox` in config)

Use `--no-sandbox` to disable, or set `sandbox: false` in `~/.clictl/config.yaml`.

## Auto-Registration on Install

When you run `clictl install`, both a skill file and an MCP server entry are created by default:

```bash
clictl install open-meteo
# Creates:
#   .claude/skills/open-meteo/SKILL.md (skill file)
#   .mcp.json entry (MCP server)
```

Use flags to control what gets created:

```bash
clictl install open-meteo --no-mcp     # skill only, no MCP
clictl install open-meteo --no-skill   # MCP only, no skill file
```

---

**See also:** [Code Mode](code-mode.md) | [Security](security.md) | [CLI Reference](cli-reference.md) | [Spec Format](spec-format.md)
