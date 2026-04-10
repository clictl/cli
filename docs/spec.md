# clictl Tool Spec 1.0

The clictl tool spec is a YAML or JSON file that describes how an AI agent can discover and use a service. One spec file makes any API, MCP server, CLI tool, skill, or website searchable, installable, and executable.

---

## Part 1: Specification

This section defines the spec format. Parsers MUST accept valid specs and MUST reject specs that violate these rules. Key words follow RFC 2119: MUST, MUST NOT, SHOULD, MAY.

### 1.1 File Format

A spec file MUST be valid YAML (`.yaml`, `.yml`) or JSON (`.json`). YAML is RECOMMENDED for readability. All examples in this document use YAML.

### 1.2 Required Fields

Every spec MUST contain these four fields at the root level:

| Field | Type | Description |
|-------|------|-------------|
| `spec` | string | MUST be `"1.0"` |
| `name` | string | Tool identifier. MUST be lowercase, may contain hyphens. No spaces, no uppercase. |
| `protocol` | string | MUST be one of: `http`, `mcp`, `skill`, `website`, `command` |
| `description` | string | Human-readable description. SHOULD be one sentence. |

```yaml
spec: "1.0"
name: open-meteo
protocol: http
description: Free weather API with current conditions, forecasts, and historical data
```

### 1.3 Protocol Rules

The `protocol` field determines which other fields are valid.

| Protocol | Required Fields | Purpose |
|----------|----------------|---------|
| `http` | `server.url`, at least one action with `method` and `path` | REST and GraphQL APIs |
| `mcp` | `server.command` + `server.args` OR `package` block | MCP stdio servers |
| `skill` | `source` block with `repo` | Agent skills (instructions + code) |
| `website` | `server.url`, at least one action | Web pages for agent consumption |
| `command` | At least one action with `run` | CLI tools wrapped for agents |

A parser MUST reject a spec where the protocol does not match the structure. For example, `protocol: http` without `server.url` is invalid.

### 1.4 Optional Metadata Fields

| Field | Type | Description |
|-------|------|-------------|
| `version` | string | Spec version (e.g., `"1.0"`, `"2.3.1"`). |
| `category` | string | Primary category (e.g., `developer`, `weather`, `ai`). |
| `tags` | string[] | Searchable tags. |
| `instructions` | string | Markdown guidance for agents using this tool. |
| `publisher` | object | Attribution. See Section 1.11. |

### 1.5 Server

The `server` block tells clictl how to reach the service.

**HTTP and Website protocols:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `url` | string | Yes | Base URL for all actions. |
| `headers` | map | No | Default headers sent with every request. |
| `timeout` | string | No | Request timeout (e.g., `"30s"`, `"2m"`). Default: `30s`. |

```yaml
server:
  url: https://api.example.com/v1
  headers:
    Accept: application/json
  timeout: 30s
```

**MCP protocol (explicit server):**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `command` | string | Yes | Binary to execute. |
| `args` | string[] | No | Command arguments. |
| `env` | map | No | Environment variables for the subprocess. |
| `timeout` | string | No | Per-request timeout. Default: `30s`. |
| `requires` | object[] | No | System dependencies (name, check command, install URL). |

```yaml
server:
  command: npx
  args: ["-y", "@modelcontextprotocol/server-filesystem", "/home"]
  env:
    HOME: /home/user
  timeout: 45s
  requires:
    - name: node
      check: node --version
      url: https://nodejs.org
```

**MCP protocol (from package):**

When a `package` block is present, the server is synthesized automatically. No `server` block is needed.

### 1.6 Package

The `package` block declares an MCP server distributed via a package manager. The CLI resolves the command automatically (e.g., `npx @pkg@version` for npm, `uvx pkg==version` for pypi).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `registry` | string | Yes | Package registry: `npm`, `pypi`. |
| `name` | string | Yes | Package name. |
| `version` | string | Yes | Package version. |
| `manager` | string | No | Override the runner (e.g., `bunx` instead of `npx`). Inferred from registry if omitted. |
| `runtime` | string | No | Runtime needed: `node`, `python`, `rust`, `go`. Informational. |

```yaml
package:
  registry: npm
  name: "@modelcontextprotocol/server-github"
  version: 2025.4.8
  runtime: node
```

### 1.7 Auth

The `auth` block describes how to authenticate requests. It MAY appear at the spec level (applies to all actions) or at the action level (overrides spec-level auth for that action).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `env` | string or string[] | Yes | Environment variable name(s). Also used as vault key. |
| `header` | string | No | Header template. Format: `"HeaderName: ${ENV_VAR}"`. |
| `param` | string | No | Query parameter name. Value is `${ENV_VAR}`. |

Exactly one of `header` or `param` SHOULD be set for HTTP/website protocols. For MCP protocol, only `env` is needed (the variable is passed to the subprocess environment).

```yaml
# Bearer token
auth:
  env: STRIPE_KEY
  header: "Authorization: Bearer ${STRIPE_KEY}"

# API key header
auth:
  env: NEWS_API_KEY
  header: "X-Api-Key: ${NEWS_API_KEY}"

# Query parameter
auth:
  env: API_KEY
  param: key

# MCP env passthrough
auth:
  env: GITHUB_PERSONAL_ACCESS_TOKEN
```

### 1.8 Actions

The `actions` array describes what the tool can do. Each action is an operation the agent can invoke.

**For HTTP and website protocols**, actions define HTTP requests:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Action identifier. Kebab-case. |
| `description` | string | Yes | What this action does. |
| `method` | string | No | HTTP method. Default: `GET`. |
| `path` | string | No | URL path appended to `server.url`. |
| `params` | object[] | No | Parameters. See Section 1.9. |
| `auth` | object | No | Action-level auth override. |
| `transform` | object[] | No | Response transforms. See Section 1.10. |
| `mutable` | boolean | No | If true, this action modifies state. User confirmation required. |
| `pagination` | object | No | Pagination config for list endpoints. |
| `retry` | object | No | Retry config (on, max_attempts, backoff, delay). |
| `assert` | object[] | No | Response assertions (status codes, field existence). |
| `stream` | boolean | No | If true, stream the response line by line. |

```yaml
actions:
  - name: list-customers
    description: List all customers with pagination
    method: GET
    path: /v1/customers
    params:
      - name: limit
        type: int
        default: "10"
        description: Number of results (1-100)
    transform:
      - type: json
        extract: "$.data"
      - type: truncate
        max_items: 20
```

**For MCP protocol**, actions are metadata for search and documentation. The actual tools are discovered from the MCP server at runtime. Action definitions in the spec override server descriptions when names match.

**For command protocol**, actions define shell commands:

| Field | Type | Description |
|-------|------|-------------|
| `run` | string | Shell command template with `${param}` substitution. |

**For skill protocol**, actions are not used. Skills are defined by their `source` block.

### 1.9 Params

Each parameter describes an input to an action.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Parameter name. Snake_case. |
| `type` | string | No | `string` (default), `int`, `float`, `bool`, `array`. |
| `required` | boolean | No | Default: `false`. |
| `description` | string | No | What this parameter does. |
| `default` | string | No | Default value if not provided. |
| `in` | string | No | Where the param goes: `query` (default for GET), `body` (default for POST/PUT/PATCH), `path`, `header`. Auto-detected from method and path template. |
| `example` | string | No | Example value for documentation. |

Path parameters are auto-detected when the action path contains `{param_name}` or `{{param_name}}`.

### 1.10 Transforms

Transforms process the response before returning it to the agent. They run in order as a pipeline.

**Common transforms:**

| Type | Key Fields | Description |
|------|-----------|-------------|
| `json` | `extract`, `select`, `rename` | JSONPath extraction, field selection, renaming |
| `truncate` | `max_items`, `max_length` | Limit array size or string length |
| `template` | `template` | Go template formatting |
| `format` | `template` | Format each item in an array |
| `html_to_markdown` | (none) | Convert HTML to markdown |

**Data processing transforms:**

| Type | Key Fields | Description |
|------|-----------|-------------|
| `sort` | `field`, `order` | Sort array by field (asc/desc) |
| `filter` | `filter` | Filter array by expression (e.g., `.score > 10`) |
| `unique` | `field` | Deduplicate by field |
| `group` | `field` | Group array by field |
| `count` | (none) | Return array length |
| `join` | `separator` | Join array into string |
| `split` | `separator` | Split string into array |
| `flatten` | (none) | Flatten nested arrays |
| `unwrap` | (none) | Unwrap single-element arrays |

**Advanced transforms:**

| Type | Key Fields | Description |
|------|-----------|-------------|
| `js` | `script` | Sandboxed JavaScript execution |
| `redact` | `patterns` | Pattern-based string replacement |
| `cost` | `max_tokens` | Token budget truncation |
| `pipe` | `tool`, `action` | Route data through another clictl tool |
| `xml_to_json` | (none) | Convert XML to JSON |
| `csv_to_json` | `headers` | Convert CSV to JSON |
| `base64_decode` | `field` | Decode base64 fields |
| `prompt` | `value` | Prepend prompt text |
| `date_format` | `field`, `from`, `to` | Reformat date fields |

Shorthand: `extract: "$.data"` is equivalent to `type: json` with `extract: "$.data"`.

```yaml
transform:
  - extract: "$.data.items"
  - type: truncate
    max_items: 20
  - type: json
    select: [name, id, status]
```

### 1.11 Publisher

Optional attribution for the spec author.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Publisher name. |
| `url` | string | Publisher website. |

```yaml
publisher:
  name: Anthropic
  url: https://anthropic.com
```

The `verified` flag is set by the registry, not by spec authors.

### 1.12 Source (Skill Protocol)

The `source` block tells clictl where to fetch skill files.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `repo` | string | Yes | GitHub repo in `org/repo` format. |
| `path` | string | No | Path within repo. |
| `ref` | string | No | Branch or tag. |
| `files` | object[] | No | File list with `path` and `sha256` for integrity verification. |

```yaml
source:
  repo: anthropic/clictl-skills
  path: skills/pdf
  ref: main
  files:
    - path: SKILL.md
      sha256: abc123...
```

### 1.13 Sandbox

Optional security constraints for MCP server subprocesses.

| Field | Type | Description |
|-------|------|-------------|
| `env.allow` | string[] | Environment variables to pass through to subprocess. |
| `network.allow` | string[] | Allowed network hosts. |
| `filesystem.read` | string[] | Allowed read paths. |
| `filesystem.write` | string[] | Allowed write paths. |

```yaml
sandbox:
  env:
    allow: [GITHUB_PERSONAL_ACCESS_TOKEN]
  network:
    allow: [api.github.com]
```

### 1.14 Extensions

Fields prefixed with `x-` are extensions. Parsers MUST preserve them but MUST NOT validate them. Third-party tools MAY use them for custom metadata.

```yaml
x-funding: https://github.com/sponsors/example
x-ai-notes: Works best with models that support function calling
```

### 1.15 MCP-Specific Fields

These fields control how MCP tools are exposed to agents:

| Field | Type | Description |
|-------|------|-------------|
| `allow` | string[] | Glob patterns. Only matching tools are exposed. |
| `deny` | string[] | Glob patterns. Matching tools are hidden. `deny` takes priority over `allow`. |
| `prompts` | object | Agent guidance. `system` (string) and `tool_instructions` (map of tool name to instruction). |
| `resources` | object | MCP resource exposure config. |
| `transforms` | map | Per-tool transforms, keyed by tool name. Applied to MCP tool output. |

```yaml
deny:
  - "drop_*"
  - "truncate_*"

transforms:
  search_repositories:
    - type: truncate
      max_items: 20
```

---

## Part 2: Guide

### Which protocol should I use?

```
Wrapping a REST API?              -> protocol: http
Registering an MCP server?        -> protocol: mcp
Adding agent skills/instructions? -> protocol: skill
Scraping a website for agents?    -> protocol: website
Wrapping a CLI tool?              -> protocol: command
```

### Minimal examples

**HTTP API (10 lines)**
```yaml
spec: "1.0"
name: hackernews
protocol: http
description: Hacker News API
server:
  url: https://hacker-news.firebaseio.com/v0
actions:
  - name: top
    description: Get top story IDs
    path: /topstories.json
```

**MCP server from npm (8 lines)**
```yaml
spec: "1.0"
name: github-mcp
protocol: mcp
description: GitHub API access via MCP
package:
  registry: npm
  name: "@modelcontextprotocol/server-github"
  version: 2025.4.8
```

**MCP server from pypi (8 lines)**
```yaml
spec: "1.0"
name: sqlite-mcp
protocol: mcp
description: SQLite database access
package:
  registry: pypi
  name: mcp-server-sqlite
  version: 2025.4.25
```

**Skill (9 lines)**
```yaml
spec: "1.0"
name: pdf
protocol: skill
description: Read and extract text from PDF files
source:
  repo: anthropic/clictl-skills
  path: skills/pdf
  files:
    - path: SKILL.md
```

**Website scraper (11 lines)**
```yaml
spec: "1.0"
name: hacker-news-scraper
protocol: website
description: Scrape Hacker News front page as markdown
server:
  url: https://news.ycombinator.com
actions:
  - name: get-front-page
    description: Get the front page
    path: /
    transform:
      - type: html_to_markdown
```

**CLI wrapper (10 lines)**
```yaml
spec: "1.0"
name: git-status
protocol: command
description: Show git working tree status
actions:
  - name: status
    description: Show changed files
    run: git status --short
  - name: diff
    description: Show unstaged changes
    run: git diff
```

### Adding auth

Most APIs need a key. clictl resolves keys from environment variables or the encrypted vault.

```yaml
# API key in a header
auth:
  env: OPENAI_API_KEY
  header: "Authorization: Bearer ${OPENAI_API_KEY}"

# API key as a query parameter
auth:
  env: WEATHER_KEY
  param: appid

# MCP server needs a token (passed as env var to subprocess)
auth:
  env: GITHUB_PERSONAL_ACCESS_TOKEN
```

Tell the user how to set the key:
```bash
clictl vault set OPENAI_API_KEY sk-...
# or
export OPENAI_API_KEY=sk-...
```

### Adding transforms

Transforms clean up API responses for agents. They run in order.

```yaml
# Extract nested data, keep specific fields, limit results
transform:
  - extract: "$.data.items"
  - type: json
    select: [name, id, status]
  - type: truncate
    max_items: 20

# Convert HTML to markdown (for website protocol)
transform:
  - type: html_to_markdown

# Format output with a template
transform:
  - type: template
    template: "{{.name}} ({{.id}}): {{.status}}"
```

### Adding pagination

For APIs that return results across multiple pages:

```yaml
pagination:
  type: page           # page, cursor, or offset
  param: page          # query parameter name
  per_page_param: limit
  per_page_default: 20
  max_pages: 10
```

Cursor-based pagination:
```yaml
pagination:
  type: cursor
  param: starting_after
  cursor_path: "$.data[-1].id"
  has_more_path: "$.has_more"
```

### Validating your spec

```bash
# Validate a single spec
clictl toolbox validate path/to/spec.yaml

# IDE validation (add to top of YAML file)
# yaml-language-server: $schema=https://clictl.dev/spec/1.0/schema.json
```

### Publishing your spec

```bash
# Test it works
clictl run my-tool my-action --param value

# Publish to the registry
clictl publish my-tool.yaml
```

Or contribute to the community toolbox:
1. Fork `github.com/clictl/toolbox`
2. Add `toolbox/{first-letter}/{tool-name}/{tool-name}.yaml`
3. Run `clictl toolbox validate`
4. Open a pull request
