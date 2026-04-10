# Spec Format

Tool specs are YAML files that describe how to call an API, scrape a website, run a CLI tool, or connect to an MCP server. Each action defines its own HTTP method, URL, path, headers, and auth.

Full field reference: [Spec 1.0 Reference](spec-reference.md)

## Quick Example

```yaml
name: github
version: "1.0"
description: GitHub REST API
category: developer
tags: [github, git, repos]

auth:
  env: GITHUB_TOKEN
  header: Authorization
  value: "Bearer ${GITHUB_TOKEN}"

actions:
  - name: user
    description: Get a user profile
    url: https://api.github.com
    path: /users/{username}
    params:
      - name: username
        required: true
        in: path

  - name: repo-issues
    description: List issues for a repository
    url: https://api.github.com
    path: /repos/{owner}/{repo}/issues
    params:
      - name: owner
        required: true
        in: path
      - name: repo
        required: true
        in: path
      - name: state
        in: query
        values: [open, closed, all]
```

## Action Fields

Each action defines its own request configuration directly (no nested `request` block):

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | required | Action name (kebab-case) |
| `description` | string | | What this action does |
| `method` | string | `GET` | HTTP method |
| `url` | string | | Base URL or full URL with path included |
| `path` | string | | URL path appended to `url` (optional, use when actions share a base URL) |
| `headers` | map | | Request headers for this action |
| `auth` | object | | Auth config (overrides top-level `auth`) |
| `params` | list | | Input parameters |
| `output` | string | `json` | Output format: json, text, html, markdown, csv |
| `mutable` | bool | `false` | Whether this action changes state |
| `transform` | list | | Response transform pipeline |
| `assert` | list | | Response validation rules |
| `steps` | list | | Composite sub-steps (presence implies composite) |

## Multi-API Specs

Each action can target a different API endpoint with different auth. One spec can cover an entire platform:

```yaml
name: acme-platform
version: "2.0"
description: Acme SaaS platform

actions:
  - name: list-users
    url: https://api.acme.com/v2
    path: /users
    auth:
      env: ACME_API_KEY
      header: Authorization
      value: "Bearer ${ACME_API_KEY}"

  - name: list-invoices
    url: https://billing.acme.com/v1
    path: /invoices
    auth:
      env: ACME_BILLING_KEY
      header: Authorization
      value: "Bearer ${ACME_BILLING_KEY}"

  - name: health
    description: Public health check (no auth)
    url: https://api.acme.com
    path: /health
```

## Auth Inheritance

Top-level `auth` is the default for all actions. Actions can override it or omit it entirely for public endpoints. Resolution order: action auth > top-level auth > no auth.

## Website Specs

Websites use the same format with `output: html` and an `html_to_markdown` transform:

```yaml
name: hackernews
version: "1.0"
description: Hacker News
category: news

actions:
  - name: front-page
    url: https://news.ycombinator.com
    output: html
    transform:
      - type: html_to_markdown
        remove_images: true
```

## Source (Skills)

Skills use a `source` block to point to a Git repository containing the skill files:

```yaml
name: screenshot
version: "1.0"
description: Capture desktop or system screenshots
category: developer
tags: [skill, screenshot]

source:
  repo: openai/skills
  path: skills/.curated/screenshot/
  ref: main
  files:
    - sha256: abc123...
      path: SKILL.md
    - sha256: def456...
      path: scripts/take_screenshot.py
```

### Repo URL resolution

The `repo` field accepts either a short GitHub path or a full URL:

| Format | Resolved to |
|--------|-------------|
| `owner/repo` | `https://github.com/owner/repo` |
| `github.com/owner/repo` | `https://github.com/owner/repo` |
| `gitlab.com/owner/repo` | `https://gitlab.com/owner/repo` |
| `https://github.com/owner/repo` | Used as-is |
| `https://git.internal.com/org/repo` | Used as-is |
| `git@github.com:owner/repo.git` | Used as-is |

Short paths (no protocol prefix, single slash) default to GitHub. For GitLab, Bitbucket, or self-hosted repos, use the full URL.

### Source fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `repo` | string | yes | Repository URL or GitHub short path |
| `path` | string | yes | Path within the repo to the skill directory |
| `ref` | string | no | Branch, tag, or commit (default: `main`) |
| `files` | list | yes | List of files with `path` and `sha256` hash |

### System requirements

Skills can declare system requirements that must be installed:

```yaml
requires_system:
  - name: python3
    check: python3 --version
    brew: python3
  - name: gh
    check: gh --version
    url: https://cli.github.com
```

### MCP requirements

Skills that depend on MCP servers:

```yaml
requires_mcp:
  - supabase-mcp
  - filesystem
```

## WebSocket Specs

WebSocket specs connect to a WebSocket server, send a message, and collect responses. Use `server.type: websocket` with a `wss://` URL:

```yaml
name: binance
version: "1.0"
description: Binance real-time market data
category: finance

server:
  type: websocket
  url: wss://stream.binance.com:9443/ws

actions:
  - name: ticker
    description: Get real-time price ticker for a trading pair
    message: '{"method": "SUBSCRIBE", "params": ["${symbol}@ticker"], "id": 1}'
    wait: 5s
    collect: 2
    params:
      - name: symbol
        required: true
        description: Trading pair (e.g., btcusdt)
```

### WebSocket Action Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `message` | string | | JSON message to send after connecting. Supports `${param}` templating |
| `wait` | string | `5s` | How long to listen for responses (duration string) |
| `collect` | int | `1` | How many messages to collect before returning |

The executor connects, sends the templated message, collects up to `collect` messages within the `wait` duration, then disconnects. Single message responses are returned directly; multiple messages are returned as a JSON array.

Auth headers are injected into the WebSocket handshake if configured.

## Composite Actions

If an action has `steps`, it is composite. Each step is a mini-action with its own method, url, path, and auth. Steps can depend on previous steps and reference their output:

```yaml
actions:
  - name: onboard-user
    description: Create user and assign role
    url: https://api.acme.com/v2
    auth:
      env: ACME_API_KEY
      header: Authorization
      value: "Bearer ${ACME_API_KEY}"
    steps:
      - id: create
        method: POST
        path: /users
        params:
          email: "${email}"
      - id: assign-role
        method: POST
        path: /users/${create.id}/roles
        depends: [create]
        params:
          role: "member"
```

Steps inherit `url`, `auth`, and `headers` from the parent action unless they override them. Cross-API orchestration works by setting a different `url` and `auth` on individual steps.

## Transforms

Transform pipelines shape API responses before output:

| Type | Purpose |
|------|---------|
| `json` | Extract fields, select keys, rename, flatten |
| `truncate` | Limit items or string length |
| `template` | Go template formatting |
| `html_to_markdown` | Convert HTML to markdown |
| `js` | Custom JavaScript transform (sandboxed) |
| `prefix` | Prepend text to output |
| `redact` | Remove sensitive fields |
| `sort` | Sort by field |
| `filter` | jq-style filtering |
| `cost` | Estimate token cost |

See the [Transforms guide](transforms.md) for full details with examples.

## Code Generation

Generate typed TypeScript or Python SDKs from any spec:

```bash
clictl codegen github --lang typescript
clictl codegen github --lang python
```

The generated code includes typed interfaces for all parameters, JSDoc/docstrings from descriptions, and enum types from `values` fields. Used by code mode for agent execution.

---

**See also:** [Transforms](transforms.md) | [Composite Actions](composites.md) | [Code Mode](code-mode.md) | [MCP](mcp.md) | [CLI Reference](cli-reference.md) | [Security](security.md)
