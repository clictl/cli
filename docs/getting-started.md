# Getting Started with Tool Specs

This guide walks through creating your first clictl tool spec from scratch. By the end, you will have a working tool that you and your AI agent can run.

## What is a tool spec?

A tool spec is a YAML file that describes how to interact with an API, website, CLI tool, MCP server, or skill. clictl reads the spec and gives your agent (Claude Code, Cursor, etc.) the ability to call it.

Think of it as a contract: you describe the endpoints, parameters, and auth, and clictl handles the rest.

## Install clictl

```bash
curl -fsSL https://download.clictl.dev/install.sh | bash
```

Verify it works:

```bash
clictl version
clictl search weather
```

## Your first HTTP API spec

Let's create a spec for the Open-Meteo weather API (no auth required).

### 1. Create the file

```bash
mkdir my-tools && cd my-tools
```

Create `weather.yaml`:

```yaml
name: weather
version: "1.0"
description: Current weather and forecasts from Open-Meteo
category: weather
tags: [weather, forecast, temperature]

actions:
  - name: current
    description: Get current weather for a location
    url: https://api.open-meteo.com/v1/forecast
    params:
      - name: latitude
        type: string
        required: true
        description: Latitude of the location
        example: "52.52"
        in: query
      - name: longitude
        type: string
        required: true
        description: Longitude of the location
        example: "13.41"
        in: query
      - name: current_weather
        type: string
        default: "true"
        in: query
    transform:
      - type: json
        extract: "$.current_weather"
```

### 2. Test it

```bash
clictl test weather.yaml
```

This validates the spec and runs each action against the live API.

### 3. Run it

```bash
clictl run weather.yaml current --latitude 52.52 --longitude 13.41
```

You should see current weather data for Berlin.

### 4. Install it for your agent

```bash
clictl install weather.yaml
```

This creates a SKILL.md file that your agent reads, teaching it how to use the weather tool.

## Spec anatomy

Every spec has these sections:

### Top-level fields

```yaml
name: my-tool              # Kebab-case identifier
version: "1.0"             # Semver version
description: What it does  # One-line description
category: developer        # Category for browsing
tags: [api, utility]       # Searchable tags
```

### Actions

Actions are the operations your tool supports. Each action is an API call:

```yaml
actions:
  - name: get-user             # Kebab-case action name
    description: Get a user    # What this action does
    method: GET                # HTTP method (default: GET)
    url: https://api.example.com  # Base URL
    path: /users/{username}    # Path with parameter placeholders
    params:                    # Input parameters
      - name: username
        required: true
        in: path               # Where the param goes: path, query, header, body
    output: json               # Response format: json, text, html, markdown
    transform:                 # Optional: shape the response
      - type: json
        select: [id, name, email]
```

### Authentication

If the API requires auth, add an `auth` block:

```yaml
auth:
  env: GITHUB_TOKEN                    # Vault key name
  header: Authorization                # HTTP header to set
  value: "Bearer ${GITHUB_TOKEN}"      # Header value with variable
```

Store the key in the vault:

```bash
clictl vault set GITHUB_TOKEN ghp_your_token_here
```

The token is encrypted at rest and injected at request time. It never appears in the spec or in your shell history.

### Parameters

Each parameter describes an input:

```yaml
params:
  - name: query          # Parameter name
    type: string         # Type: string, int, bool, float
    required: true       # Is it required?
    description: Search  # What it does
    default: ""          # Default value
    example: "react"     # Example for documentation
    in: query            # Where it goes: query, path, header, body
    values: [a, b, c]    # Allowed values (enum)
```

### Transforms

Transforms shape API responses before your agent sees them:

```yaml
transform:
  - type: json
    extract: "$.data"           # JSONPath extraction
    select: [id, name, status]  # Keep only these fields
  - type: truncate
    max_items: 20               # Limit array length
```

Common transforms:

| Type | Purpose | Example |
|------|---------|---------|
| `json` | Extract/select fields | `extract: "$.results"` |
| `truncate` | Limit items | `max_items: 10` |
| `html_to_markdown` | Convert HTML | For website scraping |
| `template` | Format output | Go template syntax |
| `sort` | Sort results | `field: "name"` |
| `filter` | Filter results | jq-style filtering |
| `redact` | Remove fields | Hide sensitive data |

### Assertions

Validate responses before processing:

```yaml
assert:
  - type: status
    values: [200, 201]
```

## Adding auth to your spec

### Bearer token (most common)

```yaml
auth:
  env: MY_API_KEY
  header: Authorization
  value: "Bearer ${MY_API_KEY}"
```

### Query parameter

```yaml
auth:
  env: API_KEY
  param: api_key
  value: "${API_KEY}"
```

### Multiple keys

```yaml
auth:
  env: API_KEY, API_SECRET
  header: Authorization
  value: "Basic ${API_KEY}:${API_SECRET}"
```

## Multi-endpoint specs

One spec can target multiple APIs:

```yaml
actions:
  - name: list-repos
    url: https://api.github.com
    path: /user/repos
    auth:
      env: GITHUB_TOKEN
      header: Authorization
      value: "Bearer ${GITHUB_TOKEN}"

  - name: list-gists
    url: https://api.github.com
    path: /gists
    # Inherits auth from above if you add a top-level auth block
```

## Website scraping

Websites use HTTP with `output: html` and a transform:

```yaml
actions:
  - name: front-page
    url: https://news.ycombinator.com
    output: html
    transform:
      - type: html_to_markdown
        remove_images: true
```

## WebSocket specs

For real-time data streams:

```yaml
server:
  type: websocket
  url: wss://stream.example.com/ws

actions:
  - name: subscribe
    message: '{"channel": "${topic}"}'
    wait: 5s
    collect: 3
    params:
      - name: topic
        required: true
```

## CLI tool wrappers

Wrap existing CLI tools:

```yaml
server:
  type: command
  shell: bash
  requires:
    - name: docker
      check: docker --version

actions:
  - name: ps
    description: List running containers
    run: docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
```

## Skills

Skills are prompt-based tools with source files:

```yaml
source:
  repo: your-org/skills
  path: skills/my-skill/
  ref: main
  files:
    - sha256: abc123...
      path: SKILL.md

requires_system:
  - name: python3
    check: python3 --version
```

See [Skill Packs](spec-format.md) for the full guide.

## Publishing

### To the public toolbox

1. Create your spec in `toolbox/{letter}/{name}/{name}.yaml`
2. Submit a PR to [github.com/clictl/toolbox](https://github.com/clictl/toolbox)

### To your workspace

```bash
clictl login
clictl publish my-tool.yaml
```

Or use the web editor at clictl.dev (Settings > Published Tools > Create Tool).

## Next steps

- [Spec Format Reference](spec-format.md) - full field reference
- [Spec 1.0 Reference](spec-reference.md) - complete schema
- [Transforms Guide](transforms.md) - all transform types
- [CLI Reference](cli-reference.md) - all commands
