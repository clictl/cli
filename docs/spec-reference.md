# Spec 1.0 Field Reference

Complete reference for every field in the clictl tool spec format.

For a quick-start authoring guide, see [spec-format.md](spec-format.md). For transforms, composites, and code mode, see the other docs in this directory.

Spec format: YAML (for authoring) or JSON (for machine consumption). One spec per file.

## Overview

A spec describes a tool that an AI agent can discover, understand, and execute through clictl. Every spec lives in the toolbox at `{first-letter}/{tool-name}/{tool-name}.yaml`. For example, `g/github/github.yaml`.

clictl supports four tool types, all using the same spec format:

| Type | Server type | Actions defined by |
|------|------------|-------------------|
| REST API | `type: http` (optional) | `method`, `url`, and `path` directly on action |
| CLI wrapper | `type: command` | `run` with a shell command template |
| MCP server | `type: stdio` or `type: http` | Static list, dynamic discovery, or both |
| Skill | No server block | `source` block with repo and files |

The agent always sees the same thing: a list of named operations with params, descriptions, and examples.

## Spec Structure

```yaml
spec: "1.0"

# --- Identity (required) ---
name: my-tool
namespace: publisher-name
description: One-line description of what the tool does
version: "1.0"
category: developer
tags: [api, example]

# --- Server (required for API/MCP/CLI, omit for skills) ---
server:
  # ...

# --- Auth (optional, omit for no-auth tools) ---
auth:
  # ...

# --- Instructions (optional, strongly recommended) ---
instructions: |
  When to use this tool and what to watch out for.

# --- Dependencies (optional) ---
depends:
  # ...

# --- Pricing (optional) ---
pricing:
  # ...

# --- Privacy (optional) ---
privacy:
  # ...

# --- Actions (required) ---
actions:
  # ...
```

## Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Unique tool name in kebab-case. Must match the filename. |
| `description` | string | Clear one-line description. Used in search results. |
| `version` | string | Spec version, e.g. `"1.0"`. Quoted to prevent YAML float parsing. |
| `category` | string | Primary category: `developer`, `data`, `devops`, `geo`, `productivity`, `ai`, `utilities`, etc. |
| `tags` | list | Search tags. Include the tool name, protocol, and key concepts. |

## Optional Top-Level Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `spec` | string | `"1.0"` | Spec format version. |
| `namespace` | string | - | Publisher identity for marketplace display. Not the resolution namespace. |
| `instructions` | string | - | Markdown guidance for agents: when to use, when not to use, rate limits, gotchas. |
| `canonical` | string | - | Source URL for this spec. Used by `clictl audit` to check divergence. |
| `deprecated` | bool | `false` | Whether the tool is deprecated. |
| `deprecated_message` | string | - | Human-readable deprecation notice. |
| `deprecated_by` | string | - | Name of the replacement tool. |

---

## Server Block

One block, three types. The `type` field is explicit.

### HTTP (REST APIs, GraphQL, remote MCP)

```yaml
server:
  type: http
  url: https://api.github.com
  headers:
    Accept: application/vnd.github+json
    X-GitHub-Api-Version: "2022-11-28"
  timeout: 15s
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `type` | string | (required) | `http` |
| `url` | string | (required) | Base URL for all requests. |
| `headers` | map | - | Headers sent with every request. |
| `timeout` | string | `30s` | Connection timeout. Duration string (e.g. `10s`, `1m`). |

### stdio (MCP servers, local processes)

```yaml
server:
  type: stdio
  command: npx
  args: ["-y", "@modelcontextprotocol/server-filesystem", "/home"]
  env:
    NODE_ENV: production
  requires:
    - name: node
      check: "node --version"
      url: https://nodejs.org
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `type` | string | (required) | `stdio` |
| `command` | string | (required) | Binary to execute. |
| `args` | list | - | Command-line arguments. |
| `env` | map | - | Environment variables passed to the process. Supports `${KEY}` vault references. |
| `requires` | list | - | System binary requirements. Each has `name`, `check` (command to verify), and `url` (install link). |

### Command (CLI wrappers)

```yaml
server:
  type: command
  shell: bash
  requires:
    - name: docker
      check: "docker --version"
      url: https://docs.docker.com/get-docker/
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `type` | string | (required) | `command` |
| `shell` | string | `bash` | Shell used to execute `run` commands. |
| `requires` | list | - | Same as stdio `requires`. |

### WebSocket (real-time data streams)

```yaml
server:
  type: websocket
  url: wss://stream.binance.com:9443/ws
  headers:
    X-Custom-Header: value
  timeout: 10s
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `type` | string | (required) | `websocket` |
| `url` | string | (required) | WebSocket URL (`wss://` or `ws://`). |
| `headers` | map | - | Headers sent during the WebSocket handshake. |
| `timeout` | string | `10s` | Handshake timeout. |

Actions on WebSocket specs use these additional fields:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `message` | string | - | JSON message to send after connecting. Supports `${param}` templating. |
| `wait` | string | `5s` | Duration to listen for responses. |
| `collect` | int | `1` | Number of messages to collect before returning. |

---

## Auth Block

Template model. `env` names the vault keys. `header`/`param` + `value` describes exactly what gets sent. No implicit behavior, no magic prefixes.

### Bearer token

```yaml
auth:
  env: GITHUB_TOKEN
  header: Authorization
  value: "Bearer ${GITHUB_TOKEN}"
```

### API key in custom header

```yaml
auth:
  env: ANTHROPIC_API_KEY
  header: x-api-key
  value: "${ANTHROPIC_API_KEY}"
```

### API key in query string

```yaml
auth:
  env: NASA_API_KEY
  param: api_key
  value: "${NASA_API_KEY}"
```

### Multiple headers

```yaml
auth:
  env: [DD_API_KEY, DD_APP_KEY]
  headers:
    DD-API-KEY: "${DD_API_KEY}"
    DD-APPLICATION-KEY: "${DD_APP_KEY}"
```

### OAuth2

```yaml
auth:
  type: oauth2
  env: SLACK_TOKEN
  header: Authorization
  value: "Bearer ${SLACK_TOKEN}"
  scopes: [channels:read, chat:write]
```

The OAuth flow is user-initiated: `clictl connect <tool>` opens the browser, user authorizes, token is stored in the vault. On subsequent runs, `${SLACK_TOKEN}` resolves from the vault.

### No auth

Omit the `auth` block entirely.

### Auth fields

| Field | Type | Description |
|-------|------|-------------|
| `env` | string or list | Vault key name(s) that clictl needs to resolve. |
| `header` | string | Single header name to set. Mutually exclusive with `headers`. |
| `headers` | map | Multiple headers to set. Mutually exclusive with `header`. |
| `param` | string | Query parameter name (for query string auth). |
| `value` | string | Template with `${KEY}` placeholders. What you write is what gets sent. |
| `type` | string | `oauth2` for OAuth flows. Omit for simple key-based auth. |
| `scopes` | list | OAuth2 scopes (only with `type: oauth2`). |

### How auth resolution works

1. `env` lists the vault key names clictl needs
2. `clictl info <tool>` shows which keys are set and which are missing
3. At runtime, `${KEY}` in `value` is replaced with the resolved secret
4. Resolution order: project vault, user vault, workspace vault, environment variable
5. The composed header/param is sent with the request

---

## Actions

Every action has the same agent-facing fields. The execution details differ by server type.

### Common fields (all action types)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | (required) | Action name in snake_case or kebab-case. |
| `description` | string | (required) | What this action does. One line. |
| `output` | string | `json` | Output format: `json`, `text`, `html`, `xml`, `csv`. |
| `mutable` | bool | `false` | Whether this action changes state. Only include when `true`. |
| `instructions` | string | - | Action-specific guidance for the agent. |
| `params` | list | - | Input parameters. See Params section. |
| `response` | object | - | Example output and description. See Response section. |
| `assert` | list | - | Response validation rules. See Asserts section. |
| `transform` | list | - | Output transformation pipeline. See Transforms section. |
| `retry` | object | - | Retry configuration. See Retry section. |
| `pagination` | object | - | Pagination configuration. See Pagination section. |
| `stream` | bool | `false` | Whether to stream the response. |
| `stream_timeout` | string | `30s` | Idle timeout for streaming actions. |
| `deprecated` | bool | `false` | Whether this action is deprecated. |
| `deprecated_by` | string | - | Name of the replacement action. |

### HTTP actions

Define `method`, `url`, and `path` directly on the action. Method defaults to GET. The `server.url` is used as a fallback if the action omits `url`.

```yaml
actions:
  - name: search_repos
    description: Search repositories across all of GitHub
    output: json
    url: https://api.github.com
    path: /search/repositories
    params:
      - name: q
        required: true
        example: "language:go stars:>100 cli"
    assert:
      - type: status
        values: [200]
    transform:
      - type: json
        extract: "$.items"
        select: [full_name, description, language, stargazers_count]
        rename: { stargazers_count: stars }
```

**POST with body params:**

```yaml
actions:
  - name: create_message
    description: Send a message to the Claude API
    mutable: true
    method: POST
    url: https://api.anthropic.com
    path: /v1/messages
    params:
      - name: model
        required: true
        example: "claude-sonnet-4-20250514"
      - name: max_tokens
        type: int
        required: true
        example: "1024"
      - name: messages
        type: array
        required: true
    assert:
      - type: status
        values: [200]
    transform:
      - type: json
        extract: "$.content[0].text"
```

**Path parameters** are detected from `{name}` syntax in the path. No `in: path` needed:

```yaml
  - name: get_repo
    url: https://api.github.com
    path: /repos/{owner}/{repo}
    params:
      - name: owner
        required: true
        example: "anthropics"
      - name: repo
        required: true
        example: "claude-code"
```

**Multi-API actions** can target different URLs and auth per action:

```yaml
actions:
  - name: list-users
    url: https://api.acme.com/v2
    path: /users
    auth:
      env: ACME_KEY
      header: Authorization
      value: "Bearer ${ACME_KEY}"

  - name: list-invoices
    url: https://billing.acme.com/v1
    path: /invoices
    auth:
      env: BILLING_KEY
      header: Authorization
      value: "Bearer ${BILLING_KEY}"
```

### HTTP action fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `method` | string | `GET` | HTTP method: GET, POST, PUT, PATCH, DELETE |
| `url` | string | `server.url` | Base URL or full URL for this action |
| `path` | string | - | URL path appended to `url` (optional if full URL in `url`) |
| `headers` | map | `server.headers` | Request headers (merged with server-level headers) |
| `auth` | object | top-level `auth` | Auth config for this action (overrides top-level) |

### CLI actions

Add a `run` field with a shell command template:

```yaml
actions:
  - name: ps
    description: List running containers
    output: text
    run: "docker ps --format 'table {{.ID}}\t{{.Names}}\t{{.Status}}\t{{.Image}}'"

  - name: logs
    description: Show container logs
    output: text
    run: "docker logs {{name}} --tail {{lines}}"
    params:
      - name: name
        required: true
        example: "my-app"
      - name: lines
        type: int
        default: "100"

  - name: stop
    description: Stop a running container
    mutable: true
    run: "docker stop {{name}}"
    params:
      - name: name
        required: true
        example: "my-app"
```

Template vars `{{name}}` are replaced from params. No `request` block needed.

### MCP static actions

No execution block. The action name maps to an MCP tool call. clictl routes the call through the MCP protocol because the server block has `type: stdio` or is an MCP endpoint.

```yaml
actions:
  - name: read_file
    description: Read file contents
    output: text
    params:
      - name: path
        required: true
        example: "./src/main.ts"
    response:
      example: |
        import express from 'express'
        const app = express()
```

---

## Params

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | (required) | Parameter name. |
| `type` | string | `string` | Type: `string`, `int`, `float`, `bool`, `array`, `object`. |
| `required` | bool | `false` | Whether the param is required. Omit unless `true`. |
| `description` | string | - | What this param does. |
| `example` | string | - | Example value. Strongly recommended. |
| `default` | string | - | Default value if not provided. Always a string (clictl coerces to `type`). |
| `values` | list | - | Allowed values (enum). clictl validates at runtime. |
| `in` | string | (auto) | Where the param is sent. Usually auto-detected; see below. |

### Param location (`in`) defaults

| Context | Default `in` | Notes |
|---------|-------------|-------|
| GET request | `query` | Omit `in` for query params. |
| POST/PUT/PATCH request | `body` | Omit `in` for body params. |
| `{name}` in path | `path` | Auto-detected. Never needs to be specified. |
| Deviation from default | Explicit | Only specify `in` when it differs from the default. |

### Param enums

```yaml
params:
  - name: sort
    values: [stars, forks, updated, created]
    default: "stars"
```

The agent sees the allowed values. clictl validates at runtime.

---

## Response Block

Helps agents understand what they will get back. Optional but strongly recommended.

```yaml
response:
  description: |
    Returns an array of matching repositories. Each entry includes
    the full name, description, primary language, and star count.
    Results are sorted by best match unless `sort` is specified.
  example: |
    [
      {
        "full_name": "anthropics/claude-code",
        "description": "CLI for Claude",
        "language": "TypeScript",
        "stars": 25000
      }
    ]
```

| Field | Type | Description |
|-------|------|-------------|
| `description` | string | What the response contains and how to interpret it. |
| `example` | string | Example output **after transforms are applied**, not the raw API response. |

The `example` shows what the agent will actually see. This is critical for agents learning the output shape.

---

## Transforms

Transforms are applied in order as a pipeline. Each step takes the output of the previous step. Every step has a `type` field and type-specific settings.

```
request -> retry on transient error -> assert -> transform -> output
```

### JSON transforms

Operate on structured JSON data. Multiple operations can combine in one step.

```yaml
# Extract a value from nested JSON using JSONPath
- type: json
  extract: "$.data.results"

# Keep only listed fields
- type: json
  select: [name, email, created_at]

# Rename fields for readability
- type: json
  rename:
    stargazers_count: stars
    html_url: url

# Combined in one step
- type: json
  extract: "$.items"
  select: [full_name, description, language, stargazers_count]
  rename: { stargazers_count: stars }

# Add default fields to every object
- type: json
  inject:
    source: "github"

# Allowlist top-level keys on root object
- type: json
  only: [results, total_count]

# Flatten nested arrays
- type: json
  flatten: true               # [[1,2],[3,4]] -> [1,2,3,4]

# Unwrap single-item arrays
- type: json
  unwrap: true                # [{"name":"x"}] -> {"name":"x"}

# Fill missing fields with defaults
- type: json
  default:
    language: "unknown"
    stars: 0
```

### Array transforms

```yaml
# Sort by field
- type: sort
  field: stars
  order: desc                 # asc or desc

# Filter items by condition
- type: filter
  jq: ".stars > 100"          # jq expression, true/false per item

# Deduplicate by field
- type: unique
  field: name

# Group by field value
- type: group
  field: language             # {"Go": [...], "Rust": [...]}

# Count items
- type: count                 # returns integer

# Join array into string
- type: join
  separator: ", "             # ["a","b","c"] -> "a, b, c"

# Split string into array
- type: split
  separator: ","              # "a,b,c" -> ["a","b","c"]
```

### Size limits

```yaml
- type: truncate
  max_items: 20               # Limit array length
  max_length: 8000            # Limit string length (characters)
```

### Text transforms

```yaml
# Convert HTML to markdown
- type: html_to_markdown
  remove_images: true
  remove_links: false

# Strip markdown to plain text
- type: markdown_to_text

# Format each item using simple template
- type: format
  template: "- {full_name} ({language}, {stars} stars)"

# Go text/template for complex logic
- type: template
  template: |
    {{range .}}{{.full_name}} ({{if .stars}}{{.stars}} stars{{end}})
    {{end}}

# Add a prefix to the output
- type: prefix
  value: "Results from GitHub:\n\n"

# Reformat dates
- type: date_format
  field: created_at
  from: "2006-01-02T15:04:05Z"     # Go time layout or "iso8601"
  to: "Jan 2, 2006"
```

### Data format conversion

```yaml
# Parse XML to JSON
- type: xml_to_json

# Parse CSV to JSON array
- type: csv_to_json
  headers: true               # First row is headers

# Decode base64 content
- type: base64_decode
  field: content              # Decode a specific field, or omit for entire response
```

### Security

```yaml
# Redact sensitive fields
- type: redact
  patterns:
    - field: "*.email"
      replace: "[redacted]"
    - field: "*.api_key"
      replace: "[redacted]"
```

### Metering

```yaml
# Track token/cost usage from AI API responses
- type: cost
  input_tokens: "$.usage.input_tokens"
  output_tokens: "$.usage.output_tokens"
  model: "$.model"
```

### External tools (pipe)

```yaml
# Pipe through another clictl tool
- type: pipe
  tool: jq
  action: filter
  params:
    filter: "[.[] | {name, stars}]"

# Pipe with simple syntax
- type: pipe
  run: "jq filter --filter '[.[] | select(.stars > 100)]'"
```

The piped tool must be listed in `depends`.

### Code transforms

```yaml
# jq expression
- type: jq
  filter: "[.[] | {name: .full_name, stars: .stargazers_count}] | sort_by(-.stars)"

# JavaScript (sandboxed)
- type: js
  script: |
    function transform(data) {
      return data.filter(r => r.score > 0.5);
    }
```

Use `jq` and `js` as escape hatches when declarative transforms are not enough.

### Prompt injection

Inject agent-facing guidance into the output. The prompt text is appended to the transformed data.

```yaml
# Unconditional: always append guidance
- type: prompt
  value: "Results are sorted by relevance. Use --sort stars for popularity."

# Conditional: only when the response matches
- type: prompt
  when: "size(data) == 0"
  value: "No results found. Try broadening the search or using different keywords."

# Dynamic: reference response fields
- type: prompt
  when: "data.rate_limit_remaining < 100"
  value: "Rate limit is low ({{rate_limit_remaining}} remaining). Space out requests."
```

### Transform lifecycle

Transforms run at different points in the action lifecycle. Most run at `on_output` (the default). Specify `on` only when overriding.

```
on_request -> Execute action -> on_response -> on_output
```

| Phase | When | Input | Use case |
|-------|------|-------|----------|
| `on_request` | Before the request is sent | Params, headers, body | Rename params, inject defaults |
| `on_response` | After response, before output transforms | Raw response | Validate, log, extract metadata |
| `on_output` | After response transforms (default) | Transformed data | Shape data for the agent |

```yaml
transform:
  # Rename agent-friendly params to API params
  - type: rename_params
    on: request
    map:
      q: query
      limit: per_page

  # Inject default params
  - type: default_params
    on: request
    values:
      format: json
      per_page: 10

  # Build a GraphQL request body
  - type: template_body
    on: request
    template: '{"query": "{ search(q: \"{q}\") { id title } }"}'

  # Shape the response (default phase, no `on` needed)
  - type: json
    extract: "$.data.items"
    select: [name, stars]
  - type: truncate
    max_items: 20
```

### MCP action transforms

For MCP servers, transforms are keyed by action name in a top-level `transforms` block:

```yaml
transforms:
  read_file:
    - type: truncate
      max_length: 8000
  list_directory:
    - type: truncate
      max_items: 100
  "*":                                  # Default for all actions
    - type: truncate
      max_length: 16000
```

### DAG transforms

Linear pipelines process steps sequentially (A -> B -> C). For parallel, branching, and merging workflows, use DAG fields on transform steps.

Every transform step can optionally have:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `id` | string | - | Name for this step's output. Required for DAG steps. |
| `input` | string or list | previous step | Which step(s) to read from. Use step `id` values. |
| `depends` | list | - | Steps that must complete first (ordering without data flow). |
| `each` | bool | `false` | Iterate over input array, run step once per item. |
| `when` | string | - | CEL expression. Step only runs if true. |

Steps without DAG fields run in linear order (backward compatible).

**Parallel enrichment:**

```yaml
transform:
  - id: repos
    type: json
    extract: "$.items"
    select: [full_name, description, language, stargazers_count]

  - id: translated
    input: repos
    type: pipe
    run: "deepl translate --target_lang DE"

  - id: formatted
    input: repos
    type: format
    template: "{full_name}: {stargazers_count} stars"

  - type: merge
    sources: [translated, formatted]
    strategy: zip
```

**Merge strategies:**

| Strategy | Description |
|----------|-------------|
| `zip` | Combine arrays item-by-item. Both inputs must be same length. |
| `concat` | Concatenate arrays. |
| `first` | Take the first non-null input. For conditional branches. |
| `join` | Join objects by a shared field (`on`). Like a SQL JOIN. |
| `object` | Combine into one object: `{translated: [...], formatted: [...]}`. |

---

## Asserts

Validate responses before transforming. Fails fast with a clear error instead of silently transforming bad data.

```yaml
assert:
  # HTTP status code check
  - type: status
    values: [200, 201]

  # JSON field checks
  - type: json
    exists: "$.data"
    not_empty: "$.data.results"

  # jq expression (returns true/false)
  - type: jq
    filter: ".data | length > 0"

  # JavaScript (return true/false)
  - type: js
    script: |
      function assert(response) {
        return response.status === 'ok' && response.data.length > 0;
      }

  # Common Expression Language
  - type: cel
    expression: "response.status_code == 200 && size(response.body.data) > 0"

  # String match on response body
  - type: contains
    value: "results"
```

**Assert types by server type:**

| Assert type | HTTP actions | CLI actions | MCP actions |
|------------|-------------|-------------|-------------|
| `status` | HTTP status code | Exit code (`values: [0]`) | Not applicable |
| `json` | Response body | stdout (if JSON) | JSON-RPC result |
| `jq` | Response body | stdout | JSON-RPC result |
| `js` | Full response object | stdout + exit code | JSON-RPC result |
| `cel` | Full response object | stdout + exit code | JSON-RPC result |
| `contains` | Response body | stdout | JSON-RPC result |

---

## Retry Block

Retry handles transient errors before assert runs. On the action, not on assert.

```yaml
retry:
  on: [429, 500, 502, 503]     # Status codes that trigger retry
  max_attempts: 3               # Total attempts (1 initial + 2 retries)
  backoff: exponential          # exponential, linear, fixed
  delay: 1s                     # Initial delay (doubles each retry for exponential)
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `on` | list | `[429, 500, 502, 503]` | Status codes or error types that trigger retry. |
| `max_attempts` | int | `3` | Total attempts including the first. |
| `backoff` | string | `exponential` | Backoff strategy: `exponential`, `linear`, `fixed`. |
| `delay` | string | `1s` | Initial delay between retries. |

For CLI actions, `on` matches exit codes. For MCP actions, `on` matches JSON-RPC error codes.

Omit the `retry` block entirely to disable retry (fail on first error). Minimal form:

```yaml
retry:
  on: [429, 500]
```

---

## Pagination Block

Declares how clictl auto-paginates with `--all`.

### Page-based

```yaml
pagination:
  type: page
  param: page
  per_page_param: per_page
  per_page_default: 30
  max_pages: 10
```

### Cursor-based

```yaml
pagination:
  type: cursor
  param: starting_after
  cursor_path: "$.data[-1].id"
  has_more_path: "$.has_more"
  max_pages: 10
```

### Offset-based

```yaml
pagination:
  type: offset
  param: offset
  per_page_param: limit
  per_page_default: 25
  max_pages: 10
```

Usage: `clictl run github list_issues --all` auto-paginates.

---

## Streaming

```yaml
actions:
  - name: tail_logs
    stream: true
    stream_timeout: 30s
    run: "docker logs -f {{name}}"

  - name: watch_events
    stream: true
    stream_timeout: 60s
    url: https://api.example.com
    path: /events/stream
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `stream` | bool | `false` | Enable streaming output. |
| `stream_timeout` | string | `30s` | Idle timeout. clictl stops if no new data within this window. Set to `0` to disable. |

---

## Composite Actions

Multi-step workflows that chain API calls. If an action has `steps`, it is composite. Steps form a DAG resolved via topological sort.

```yaml
actions:
  - name: onboard-user
    description: Create a user and assign default role
    url: https://api.acme.com/v2
    auth:
      env: ACME_KEY
      header: Authorization
      value: "Bearer ${ACME_KEY}"
    params:
      - name: email
        required: true
      - name: name
        required: true
    steps:
      - id: create
        method: POST
        path: /users
        params:
          email: "${params.email}"
          name: "${params.name}"

      - id: assign-role
        method: POST
        path: /users/${steps.create.id}/roles
        depends: [create]
        params:
          role: "member"
```

Steps inherit `url`, `auth`, and `headers` from the parent action. Override any field on a step to target a different API. Steps can also delegate to other tools via `tool` and `action` fields.

### Step fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `id` | string | (required) | Unique step identifier |
| `method` | string | `GET` | HTTP method |
| `url` | string | parent's url | Base URL |
| `path` | string | parent's path | URL path |
| `headers` | map | parent's headers | Request headers |
| `auth` | object | parent's auth | Auth config |
| `tool` | string | - | Delegate to another tool |
| `action` | string | - | Action on the delegated tool |
| `params` | map | - | Parameters (supports `${params.*}` and `${steps.*}` expressions) |
| `depends` | list | - | Step IDs that must complete first |
| `condition` | string | - | Skip if evaluates to false |
| `on_error` | string | `fail` | `fail`, `skip`, or `continue` |
| `retry` | object | - | Retry config |

### Limits

| Limit | Value |
|-------|-------|
| Max steps | 20 |
| Max dependency depth | 3 |
| Nested composites | Not supported |

### DAG transforms with pipe

Transforms can also chain tools using `type: pipe`:

```yaml
depends:
  - deepl

actions:
  - name: search_and_translate
    description: Search repos and translate descriptions
    url: https://api.github.com
    path: /search/repositories
    params:
      - name: q
        required: true
    transform:
      - id: results
        type: json
        extract: "$.items"
        select: [full_name, description]

      - id: translated
        input: results
        type: pipe
        run: "deepl translate --target_lang DE"

      - type: truncate
        input: translated
        max_items: 5
```

Each `pipe` step runs through the referenced tool's own spec and sandbox rules.

---

## MCP Discovery

All stdio MCP servers automatically discover tools at runtime by calling `tools/list` on the server. No configuration needed.

Static `actions` in the spec serve as metadata for search indexing and documentation. At runtime, the actual tools come from the server.

```yaml
# Static actions are optional metadata for search and clictl info
actions:
  - name: query
    description: Execute a SQL query

# Control which tools are exposed to the agent
deny:
  - "drop_*"
  - "truncate_*"

allow:
  - "query"
  - "list_*"
  - "describe_*"

transforms:
  "*":
    - type: truncate
      max_items: 100
```

| Field | Type | Description |
|-------|------|-------------|
| `actions` | list | Optional. Static action metadata for search and documentation. Not used for execution. |
| `allow` | list | Glob patterns. Only matching tools are exposed to the agent. |
| `deny` | list | Glob patterns. Matching tools are never exposed. |

**How it works:**

1. Static actions in the spec are indexed for search and shown by `clictl info`
2. At runtime, clictl starts the server and calls `tools/list`
3. Spec static actions override server descriptions (spec wins for name matches)
4. Server tools not in the spec pass through as-is
5. `deny` patterns filter the result
6. `allow` patterns whitelist (if present, only matching tools pass through)
7. The agent sees one unified list

---

## Skills

Skills are prompt-based agent capabilities. They have no `server` or `actions` blocks. The `source` block tells clictl where to fetch the skill files.

```yaml
spec: "1.0"
name: pdf
namespace: anthropic
description: Extract and analyze PDF content
version: "1.0"
category: productivity
tags: [pdf, document, extract]

source:
  repo: anthropics/claude-code
  path: skills/pdf
  ref: main
  files:
    - path: SKILL.md
      sha256: a1b2c3d4e5f6...
    - path: helpers/extract.py
      sha256: f6e5d4c3b2a1...

depends:
  - poppler

sandbox:
  bash_allow: [uv, python3, pdftotext]
  filesystem:
    read: ["**/*.pdf", "**/*.md"]
    write: ["**/*.md", "**/*.txt"]
```

### Source fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `source.repo` | string | yes | Repository URL or GitHub short path (`owner/repo`). |
| `source.path` | string | yes | Directory path within the repo. |
| `source.ref` | string | no | Git ref - branch, tag, or commit (default: `main`). |
| `source.files` | list | yes | Files to fetch. Each has `path` and `sha256` hash. |

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

| Field | Type | Description |
|-------|------|-------------|
| `requires_system[].name` | string | Dependency name. |
| `requires_system[].check` | string | Shell command to verify installation. |
| `requires_system[].brew` | string | Homebrew formula (optional). |
| `requires_system[].url` | string | Install URL if brew is not available. |

### MCP requirements

Skills that depend on MCP servers:

```yaml
requires_mcp:
  - supabase-mcp
  - filesystem
```

### Sandbox fields

| Field | Type | Description |
|-------|------|-------------|
| `sandbox.bash_allow` | list | Allowed shell commands. |
| `sandbox.filesystem.read` | list | Glob patterns for allowed reads. |
| `sandbox.filesystem.write` | list | Glob patterns for allowed writes. |

**Skill trust model:**
- SHA256 hashes on every file. Mismatch aborts install.
- Toolbox trust: user explicitly adds toolbox sources.
- Sandbox constraints restrict runtime behavior.
- Skills from unverified publishers require `--trust` on first install.

---

## Dependencies

Tools can depend on other tools. Any tool type can depend on any other.

```yaml
depends:
  - jq                    # CLI tool used in transform pipeline
  - deepl                 # REST API used to translate output
  - postgres-mcp          # MCP server this tool needs running
```

Dependencies are clictl tools (by name), not system binaries. System binary requirements go in `server.requires`.

On `clictl install`, clictl checks `depends` and warns about missing dependencies. Qualified names are supported: `depends: [community/jq]`.

**Resolution order:**
1. Same toolbox as the dependent
2. Other installed toolboxes (config order)
3. Community toolbox

---

## Pricing

Signals whether a tool has costs. No dollar amounts - they go stale immediately.

```yaml
# Free tool: omit the pricing block entirely

# Paid
pricing:
  model: paid
  url: https://stripe.com/pricing

# Freemium (free tier available)
pricing:
  model: freemium
  url: https://api.openai.com/pricing

# Enterprise / contact sales
pricing:
  model: contact
  url: https://example.com/enterprise
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `model` | string | `free` | One of: `free`, `freemium`, `paid`, `contact`. |
| `url` | string | - | Where the user signs up or sees pricing. |

---

## Privacy

Optional. Primarily useful for enterprise environments where workspace admins filter tools by data handling characteristics.

```yaml
# Local tool - no data leaves the machine
privacy:
  local: true

# Tool may process personally identifiable data
privacy:
  pii: true
```

Omit entirely for typical REST APIs (it is obvious that params go to the external service).

---

## Namespace

Publisher identity for the marketplace. Not the resolution namespace (that is the toolbox source name).

```yaml
name: github
namespace: github           # "this spec was published by the GitHub organization"
```

---

## Deprecation

```yaml
deprecated: true
deprecated_message: "Use github-mcp instead"
deprecated_by: github-mcp

actions:
  - name: old_search
    deprecated: true
    deprecated_by: search_repos
```

`clictl run` warns on stderr. `clictl audit` flags deprecated installs.

---

## Canonical

```yaml
canonical: https://github.com/clictl/toolbox/blob/main/toolbox/g/github/github.yaml
```

Optional source URL. `clictl audit` checks divergence from canonical.

---

## Extensibility

Custom fields use the `x-` prefix:

```yaml
x-clictl:
  billing_sku: "tool-github-v1"
x-myplatform:
  internal_id: 12345
```

Unknown fields without `x-` are a validation warning (not error).

---

## Defaults Table

| Field | Default | Notes |
|-------|---------|-------|
| `spec` | `"1.0"` | Spec format version |
| `server.timeout` | `30s` | Connection timeout |
| `discover` | auto | Automatic for stdio servers. Not needed in spec YAML. |
| `pricing.model` | `free` | Omit pricing block for free tools |
| `params[].type` | `string` | Most params are strings |
| `params[].required` | `false` | Omit unless true |
| `params[].in` (GET) | `query` | Omit for query params |
| `params[].in` (POST/PUT/PATCH) | `body` | Omit for body params |
| `params[].in` (path match) | `path` | Auto-detected from `{name}` in path |
| `output` | `json` | Omit for JSON-returning actions |
| `mutable` | `false` | Omit unless the action changes state |
| `retry.on` | `[429, 500, 502, 503]` | Transient HTTP errors |
| `retry.max_attempts` | `3` | 1 initial + 2 retries |
| `retry.backoff` | `exponential` | Doubles each retry |
| `retry.delay` | `1s` | Initial delay |
| `stream_timeout` | `30s` | Idle timeout for streaming |

---

## Complete Examples

### REST API (no auth)

```yaml
spec: "1.0"
name: nominatim
namespace: openstreetmap
description: Geocoding service powered by OpenStreetMap. Convert addresses to coordinates.
version: "1.0"
category: geo
tags: [geocoding, maps, location, coordinates]

server:
  type: http
  url: https://nominatim.openstreetmap.org
  headers:
    Accept: application/json
    User-Agent: clictl/1.0
  timeout: 10s

instructions: |
  Free geocoding service with no API key required.
  Rate limited to 1 request per second. Do not use for bulk geocoding.

actions:
  - name: search
    description: Forward geocode an address or place name to coordinates
    method: GET
    path: /search
    params:
      - name: q
        required: true
        description: Address or place name
        example: "1600 Pennsylvania Ave, Washington DC"
      - name: format
        default: jsonv2
      - name: limit
        type: int
        default: "5"
    response:
      example: |
        [
          {
            "display_name": "White House, 1600, Pennsylvania Avenue, Washington, DC",
            "lat": "38.8976633",
            "lon": "-77.0365739",
            "type": "house"
          }
        ]
    transform:
      - type: json
        select: [display_name, lat, lon, type]
      - type: truncate
        max_items: 5

  - name: reverse
    description: Reverse geocode coordinates to an address
    method: GET
    path: /reverse
    params:
      - name: lat
        type: float
        required: true
        example: "38.8976633"
      - name: lon
        type: float
        required: true
        example: "-77.0365739"
      - name: format
        default: jsonv2
```

### REST API (with auth)

```yaml
spec: "1.0"
name: github
namespace: github
description: GitHub REST API for repositories, issues, pull requests, and users
version: "1.0"
category: developer
tags: [github, git, repos, issues, pull-requests]

pricing:
  model: freemium
  url: https://github.com/pricing

server:
  type: http
  url: https://api.github.com
  headers:
    Accept: application/vnd.github+json
    X-GitHub-Api-Version: "2022-11-28"
  timeout: 15s

auth:
  env: GITHUB_TOKEN
  header: Authorization
  value: "Bearer ${GITHUB_TOKEN}"

instructions: |
  Use for reading GitHub data: repos, issues, PRs, users.
  For git operations, use the `git` CLI tool instead.
  For GitHub Actions and advanced workflows, use the `gh` CLI.
  Rate limit: 5,000 req/hour with token, 60/hour without.

actions:
  - name: repos
    description: List public repositories for a user
    method: GET
    path: /users/{username}/repos
    params:
      - name: username
        required: true
        example: "anthropics"
      - name: sort
        default: updated
        description: "Sort by: created, updated, pushed, full_name"
      - name: per_page
        type: int
        default: "10"
    response:
      example: |
        [
          {
            "full_name": "anthropics/claude-code",
            "description": "CLI for Claude",
            "language": "TypeScript",
            "stars": 25000
          }
        ]
    transform:
      - type: json
        select: [full_name, description, language, stargazers_count]
        rename: { stargazers_count: stars }

  - name: search_repos
    description: Search repositories across all of GitHub
    method: GET
    path: /search/repositories
    instructions: |
      The `q` param uses GitHub search syntax:
      - `language:go stars:>100` - filter by language and stars
      - `org:anthropics` - scope to an organization
      - `topic:cli` - filter by topic
    params:
      - name: q
        required: true
        description: Search query using GitHub search syntax
        example: "language:go stars:>100 cli"
      - name: sort
        description: "Sort by: stars, forks, updated"
      - name: per_page
        type: int
        default: "10"
    assert:
      - type: status
        values: [200]
    transform:
      - type: json
        extract: "$.items"
        select: [full_name, description, language, stargazers_count]
        rename: { stargazers_count: stars }
```

### CLI wrapper

```yaml
spec: "1.0"
name: docker
namespace: docker
description: Docker container management
version: "1.0"
category: devops
tags: [docker, containers, images]

server:
  type: command
  shell: bash
  requires:
    - name: docker
      check: "docker --version"
      url: https://docs.docker.com/get-docker/

instructions: |
  Manage Docker containers, images, and volumes.
  Read-only operations (ps, images, logs) are safe.
  Start/stop/remove operations will prompt for confirmation.

actions:
  - name: ps
    description: List running containers
    run: "docker ps --format 'table {{.ID}}\t{{.Names}}\t{{.Status}}\t{{.Image}}'"
    output: text
    response:
      example: |
        CONTAINER ID   NAMES     STATUS         IMAGE
        a1b2c3d4e5f6   my-app    Up 2 hours     nginx:latest

  - name: logs
    description: Show recent container logs
    run: "docker logs {{name}} --tail {{lines}}"
    output: text
    params:
      - name: name
        required: true
        description: Container name or ID
        example: "my-app"
      - name: lines
        type: int
        default: "100"

  - name: stop
    description: Stop a running container
    mutable: true
    run: "docker stop {{name}}"
    output: text
    params:
      - name: name
        required: true
        example: "my-app"
```

### MCP server (dynamic discovery)

```yaml
spec: "1.0"
name: postgres-mcp
namespace: modelcontextprotocol
description: PostgreSQL database access via MCP
version: "1.0"
category: data
tags: [postgres, sql, database, query]

privacy:
  pii: true

server:
  type: stdio
  command: npx
  args: ["-y", "@modelcontextprotocol/server-postgres"]
  env:
    POSTGRES_CONNECTION_STRING: "${POSTGRES_CONNECTION_STRING}"

auth:
  env: POSTGRES_CONNECTION_STRING

instructions: |
  Provides read/write access to a PostgreSQL database.
  Always run `describe_table` before writing queries to understand the schema.
  Never DROP or TRUNCATE tables without explicit user confirmation.

# Static actions are metadata for search and documentation.
# Tools are discovered at runtime from the MCP server.
actions:
    - name: query
      description: Execute a SQL query against the connected database
      output: json
      instructions: "Always LIMIT results to 100 rows unless the user asks for more."
      params:
        - name: sql
          required: true
          example: "SELECT id, name FROM users LIMIT 10"
      response:
        example: |
          [{"id": 1, "name": "Alice"}, {"id": 2, "name": "Bob"}]

    - name: list_tables
      description: List all tables in the database
      output: json
      response:
        example: |
          ["users", "orders", "products"]

    - name: describe_table
      description: Show column names and types for a table
      instructions: "Run this before writing queries to understand the schema."
      params:
        - name: table_name
          required: true
          example: "users"

deny:
  - "drop_*"
  - "truncate_*"

transforms:
  "*":
    - type: truncate
      max_items: 100
```

### Skill

```yaml
spec: "1.0"
name: pdf
namespace: anthropic
description: Extract and analyze PDF content
version: "1.0"
category: productivity
tags: [pdf, document, extract]

source:
  repo: anthropics/claude-code
  path: skills/pdf
  ref: main
  files:
    - path: SKILL.md
      sha256: a1b2c3d4e5f6...
    - path: helpers/extract.py
      sha256: f6e5d4c3b2a1...

depends:
  - poppler

sandbox:
  bash_allow: [uv, python3, pdftotext]
  filesystem:
    read: ["**/*.pdf", "**/*.md"]
    write: ["**/*.md", "**/*.txt"]
```

### Composite (multi-tool pipeline)

```yaml
spec: "1.0"
name: github-translate
namespace: community
description: Search GitHub repos and translate descriptions
version: "1.0"
category: developer
tags: [github, translate, search]

depends:
  - deepl

server:
  type: http
  url: https://api.github.com
  headers:
    Accept: application/vnd.github+json

auth:
  env: GITHUB_TOKEN
  header: Authorization
  value: "Bearer ${GITHUB_TOKEN}"

actions:
  - name: search_and_translate
    description: Search repos and translate descriptions to German
    method: GET
    path: /search/repositories
    params:
      - name: q
        required: true
        example: "language:go cli"
    transform:
      - id: results
        type: json
        extract: "$.items"
        select: [full_name, description]

      - id: translated
        input: results
        type: pipe
        run: "deepl translate --target_lang DE"

      - type: truncate
        input: translated
        max_items: 5
```
