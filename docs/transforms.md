# Transforms

Transforms shape API responses before your agent sees them. Instead of parsing raw 50-field JSON blobs, your agent gets clean, focused output.

## How It Works

```
pre-transform -> HTTP request -> assert -> transform -> output
```

Transforms run in a pipeline. Each step receives the output of the previous step. Pre-transforms modify the request before it's sent. Assertions validate the response. Transforms shape the output.

## Quick Example

Raw API response (47 fields):
```json
{
  "coord": {"lon": -0.13, "lat": 51.51},
  "weather": [{"id": 800, "main": "Clear", "description": "clear sky"}],
  "main": {"temp": 18.5, "feels_like": 17.2, "humidity": 72, "pressure": 1013},
  "wind": {"speed": 4.12, "deg": 250},
  "sys": {"country": "GB"},
  "name": "London"
}
```

After transforms:
```
Temperature: 18.5C (feels like 17.2C)
Humidity: 72%
Wind: 4.12 m/s
```

The spec that does this:
```yaml
actions:
  - name: current
    url: https://api.openweathermap.org/data/2.5/weather
    params:
      - name: q
        required: true
        in: query
    transform:
      - type: json
        extract: "$.main"
        select: [temp, feels_like, humidity]
      - type: template
        template: |
          Temperature: {{.temp}}C (feels like {{.feels_like}}C)
          Humidity: {{.humidity}}%
```

## Transform Types

### extract

Pull a subset of data from a nested JSON response using JSONPath.

```yaml
transform:
  - type: json
    extract: "$.data.items"
```

Supports dot notation and array indexing: `$.results[0].name`, `$.data.users`.

### select

Keep only specific fields from objects or arrays of objects.

```yaml
transform:
  - type: json
    select: [name, email, role]
```

### rename

Rename fields for clarity.

```yaml
transform:
  - type: json
    rename:
      dt: date
      temp_max: high
      temp_min: low
```

### truncate

Limit array length or string length.

```yaml
transform:
  - type: truncate
    max_items: 10       # keep first 10 array items
    max_length: 500     # truncate strings to 500 chars
```

### template

Format output using Go templates.

```yaml
transform:
  - type: template
    template: |
      {{range .}}- {{.name}}: {{.status}}
      {{end}}
```

### html_to_markdown

Convert HTML content to clean markdown. Essential for website specs.

```yaml
transform:
  - type: html_to_markdown
    remove_images: true
    remove_links: false
```

### js

Custom JavaScript transform (sandboxed, no network access).

```yaml
transform:
  - type: js
    script: |
      function transform(data) {
        return data.items.map(item => ({
          name: item.name,
          score: item.stars * 2 + item.forks
        }))
      }
```

The script must define a `transform(data)` function. No `fetch`, `eval`, `require`, or timers. 5-second timeout.

### prefix

Prepend text to the output.

```yaml
transform:
  - type: prefix
    value: "Results for {{.query}}:"
```

### only

Filter items matching a condition.

```yaml
transform:
  - type: only
    filter: ".status == 'open'"
```

### inject

Add static data to the output.

```yaml
transform:
  - type: inject
    inject:
      source: "github"
      fetched_at: "2026-01-01"
```

### redact

Remove sensitive fields from output.

```yaml
transform:
  - type: redact
    patterns:
      - field: "email"
        replace: "[redacted]"
      - field: "api_key"
        replace: "****"
```

### sort

Sort arrays by a field.

```yaml
transform:
  - type: sort
    field: "created_at"
    order: desc
```

### filter (jq)

Apply a jq-style filter expression.

```yaml
transform:
  - type: filter
    filter: '.[] | select(.language == "Go")'
```

### cost

Annotate output with estimated token cost metadata.

```yaml
transform:
  - type: cost
    model: "claude-sonnet-4-20250514"
    input_tokens: "$.usage.prompt_tokens"
    output_tokens: "$.usage.completion_tokens"
```

## Pre-Transforms

Pre-transforms modify the request before it's sent. They run with `on: request`.

### default_params

Inject default values for missing parameters.

```yaml
transform:
  - type: default_params
    on: request
    default_params:
      per_page: "25"
      sort: "updated"
```

### rename_params

Rename parameters before sending.

```yaml
transform:
  - type: rename_params
    on: request
    rename_params:
      query: q
      count: per_page
```

### template_body

Build a request body from a Go template.

```yaml
transform:
  - type: template_body
    on: request
    template_body: |
      {"query": "{{.query}}", "filters": {"status": "{{.status}}"}}
```

## Assertions

Assertions validate the response. If any assertion fails, the action returns an error.

```yaml
assert:
  - type: status
    values: [200, 201]
  - type: json
    exists: "$.data"
    not_empty: "$.data.items"
  - type: contains
    value: "success"
```

| Type | What it checks |
|------|----------------|
| `status` | HTTP status code is in the `values` list |
| `json` / `exists` | JSONPath field exists in response |
| `json` / `not_empty` | JSONPath field is not empty |
| `contains` | Response body contains the string |
| `js` | Custom JavaScript validation |

## Chaining Transforms

Transforms execute in order. Each step receives the output of the previous step.

```yaml
transform:
  # Step 1: Extract the nested data
  - type: json
    extract: "$.response.data.repositories"

  # Step 2: Keep only the fields we need
  - type: json
    select: [name, stars, language]

  # Step 3: Sort by stars
  - type: sort
    field: stars
    order: desc

  # Step 4: Limit to top 5
  - type: truncate
    max_items: 5

  # Step 5: Format for the agent
  - type: template
    template: |
      Top repositories:
      {{range .}}- {{.name}} ({{.language}}) - {{.stars}} stars
      {{end}}
```

## Testing Transforms

Use `clictl transform` to test pipelines on JSON data without hitting an API:

```bash
# From flags
echo '{"data": [{"name": "a"}, {"name": "b"}]}' | clictl transform --extract '$.data'

# From a YAML file
echo '{"items": [1,2,3]}' | clictl transform --file my-transforms.yaml

# Chain with a raw API response
clictl run my-tool get-data --raw | clictl transform --extract '$.results' --truncate 5
```

Use `--raw` with `clictl run` to skip the spec's transforms and see the full API response.

---

**See also:** [Spec Format](spec-format.md) | [CLI Reference](cli-reference.md) | [Composite Actions](composites.md)
