# Composite Actions

Composite actions chain multiple API calls into a single operation. Each step can call a different URL, depend on previous steps, and reference their output.

## When to Use Composites

- **Multi-step workflows**: Create a user, then assign them a role
- **Cross-API orchestration**: Fetch data from one API, send it to another
- **Conditional logic**: Skip steps based on previous results
- **Data aggregation**: Combine results from multiple endpoints

## Basic Example

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

Run it:
```bash
clictl run acme-platform onboard-user --email dev@acme.com --name "Jane Doe"
```

## How Steps Work

Steps form a DAG (directed acyclic graph). The executor resolves dependencies via topological sort and runs steps in the correct order.

### Step fields

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique step identifier (required) |
| `method` | string | HTTP method (default: GET) |
| `url` | string | Base URL (inherits from parent action if empty) |
| `path` | string | URL path (inherits from parent action if empty) |
| `headers` | map | Request headers (inherits from parent action if empty) |
| `auth` | object | Auth config (inherits from parent action if empty) |
| `tool` | string | Delegate to another tool (instead of inline HTTP) |
| `action` | string | Action on the delegated tool |
| `params` | map | Parameters (supports template expressions) |
| `depends` | list | Step IDs that must complete first |
| `condition` | string | Skip step if expression evaluates to false or empty |
| `on_error` | string | `fail` (default), `skip`, or `continue` |
| `retry` | object | Retry config with `max_attempts`, `delay` |
| `transform` | list | Per-step transform pipeline |

### Inheritance

Steps inherit `url`, `auth`, and `headers` from the parent action. Override any field on a specific step to target a different API.

## Template Expressions

Reference input parameters and previous step output using `${...}` or `{{...}}` syntax.

### Input parameters

```yaml
params:
  city: "${params.city}"
  country: "${params.country}"
```

### Previous step output

```yaml
# Reference a field from a previous step's JSON response
path: /users/${steps.create.id}/roles

# Nested field access
params:
  latitude: "${steps.geocode.results[0].lat}"
  longitude: "${steps.geocode.results[0].lon}"
```

### Environment variables

```yaml
params:
  api_key: "${env.MY_API_KEY}"
```

## Cross-API Orchestration

Different steps can hit different URLs with different auth:

```yaml
actions:
  - name: provision-customer
    description: Create account and set up billing
    steps:
      - id: create-account
        method: POST
        url: https://api.acme.com/v2
        path: /accounts
        auth:
          env: ACME_KEY
          header: Authorization
          value: "Bearer ${ACME_KEY}"
        params:
          email: "${params.email}"
          plan: "${params.plan}"

      - id: setup-billing
        method: POST
        url: https://billing.acme.com/v1
        path: /subscriptions
        depends: [create-account]
        auth:
          env: BILLING_KEY
          header: Authorization
          value: "Bearer ${BILLING_KEY}"
        params:
          account_id: "${steps.create-account.id}"
          plan: "${params.plan}"

      - id: send-welcome
        method: POST
        url: https://hooks.acme.com/v1
        path: /notifications
        depends: [create-account]
        auth:
          env: WEBHOOK_KEY
          header: X-Webhook-Key
          value: "${WEBHOOK_KEY}"
        params:
          type: "welcome"
          email: "${params.email}"
```

Steps `setup-billing` and `send-welcome` both depend on `create-account` but are independent of each other.

## Delegating to Other Tools

Instead of inline HTTP calls, steps can call actions on other installed tools:

```yaml
actions:
  - name: deploy-and-notify
    steps:
      - id: deploy
        tool: vercel
        action: create-deployment
        params:
          project: "${params.project}"

      - id: notify
        tool: slack
        action: post-message
        depends: [deploy]
        params:
          channel: "#deployments"
          text: "Deployed ${params.project}: ${steps.deploy.url}"
```

Cross-tool steps resolve the other tool's spec from the registry. The delegated tool handles its own auth.

## Conditional Steps

Skip a step based on a condition expression:

```yaml
steps:
  - id: check-status
    url: https://api.acme.com
    path: /deployments/${params.id}/status

  - id: rollback
    method: POST
    url: https://api.acme.com
    path: /deployments/${params.id}/rollback
    depends: [check-status]
    condition: "${steps.check-status.status} == failed"

  - id: notify-failure
    tool: slack
    action: post-message
    depends: [rollback]
    condition: "${steps.check-status.status} == failed"
    params:
      channel: "#incidents"
      text: "Rolled back deployment ${params.id}"
```

## Error Handling

Control what happens when a step fails:

```yaml
steps:
  - id: primary
    url: https://api.primary.com/data
    on_error: skip       # skip this step on failure, continue pipeline

  - id: fallback
    url: https://api.backup.com/data
    on_error: continue   # store empty result, keep going

  - id: critical
    method: POST
    url: https://api.acme.com/audit
    on_error: fail       # stop the pipeline (default)
```

| Value | Behavior |
|-------|----------|
| `fail` | Stop the pipeline and return the error (default) |
| `skip` | Skip this step entirely, continue with next step |
| `continue` | Store empty result `{}`, continue with next step |

## Retry

Retry individual steps on failure:

```yaml
steps:
  - id: flaky-api
    url: https://api.unreliable.com/data
    retry:
      max_attempts: 3
      delay: "2s"
```

## Output Template

The final output is the last step's result by default (the terminal step in the DAG). Use a transform template to combine results from multiple steps:

```yaml
actions:
  - name: full-report
    steps:
      - id: users
        url: https://api.acme.com/v2/users

      - id: invoices
        url: https://billing.acme.com/v1/invoices

    transform:
      - type: template
        template: |
          Users: ${steps.users}
          Invoices: ${steps.invoices}
```

## Limits

| Limit | Value |
|-------|-------|
| Maximum steps per action | 20 |
| Maximum dependency depth | 3 |
| Nested composites | Not supported (a step cannot reference another composite action) |

---

**See also:** [Spec Format](spec-format.md) | [Transforms](transforms.md) | [CLI Reference](cli-reference.md)
