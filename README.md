# clictl

[![clictl](https://api.clictl.dev/api/v1/badge/clictl/cli/)](https://clictl.dev)
[![Go](https://img.shields.io/github/go-mod/go-version/clictl/cli)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Release](https://img.shields.io/github/v/release/clictl/cli)](https://github.com/clictl/cli/releases)

A package manager for AI agents. Install any API, MCP server, or website as a CLI command. Your agent discovers it automatically.

**Website:** [clictl.dev](https://clictl.dev) | **Spec:** [clictl.dev/spec](https://clictl.dev/spec) | **Browse tools:** [clictl.dev/browse](https://clictl.dev/browse)

## Install

**macOS / Linux:**
```bash
curl -fsSL https://download.clictl.dev/install.sh | bash
```

**Windows (PowerShell):**
```powershell
irm https://download.clictl.dev/install.ps1 | iex
```

**Homebrew:**
```bash
brew tap clictl/clictl
brew install clictl
```

## What it does

You write a YAML spec describing an API. `clictl install` turns it into a CLI command and registers it as an MCP server. Both you and your agent use the same tool, the same way.

```bash
clictl install github
clictl run github repo-issues --owner octocat --repo hello-world
```

There are 220+ tools in the [registry](https://clictl.dev/browse). Every install creates a skill file your agent reads and an MCP server entry, with no background processes or Docker containers.

## Key commands

```bash
clictl search <query>              # find tools in the registry
clictl install <tool>              # install a tool (skill + MCP)
clictl install group <name>        # install all tools in a group
clictl run <tool> <action>         # run a tool action
clictl mcp-serve [tools...]        # serve tools via MCP
clictl vault set <name> <value>    # store a secret in the encrypted vault
```

See [docs/cli-reference.md](docs/cli-reference.md) for the full command reference.

## Security

Secrets live in an encrypted vault, not env vars or config files. MCP subprocesses run in OS-level sandboxes with env scrubbing and filesystem isolation. Published specs are signed and checksummed.

Details: [docs/security.md](docs/security.md) | [docs/securing-secrets.md](docs/securing-secrets.md)

## Spec format

A spec is a YAML file with four required fields. Each action defines its own URL, method, and auth.

```yaml
spec: "1.0"
name: acme-platform
protocol: http
description: Acme Platform API
version: "2.0"

server:
  url: https://api.acme.com/v2

auth:
  env: ACME_KEY
  header: "Authorization: Bearer ${ACME_KEY}"

actions:
  - name: list-users
    description: List all users
    path: /users
```

Full spec reference: [clictl.dev/spec](https://clictl.dev/spec) | JSON Schema: [clictl.dev/spec/1.0/schema.json](https://clictl.dev/spec/1.0/schema.json)

## Documentation

[Getting started](docs/getting-started.md) | [CLI reference](docs/cli-reference.md) | [Spec format](https://clictl.dev/spec) | [Transforms](docs/transforms.md) | [Code mode](docs/code-mode.md) | [MCP](docs/mcp.md) | [Security](docs/security.md) | [Memory](docs/memory.md) | [clictl.dev/docs](https://clictl.dev/docs)

## Contributing

Contributions are welcome. Please open an issue to discuss your idea before submitting a pull request.

```bash
git clone https://github.com/clictl/cli.git && cd cli
go build ./... && go test ./...
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for details.

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.

clictl is a [Soap Bucket LLC](https://www.soapbucket.org) project. SOAPBUCKET and clictl are trademarks of Soap Bucket LLC.
