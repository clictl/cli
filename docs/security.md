# Security

How clictl handles credentials, tokens, and secrets. Designed so sensitive data never leaks into tool specs, logs, or version control.

## Principles

1. **Secrets stay in environment variables or .env files.** Tool specs reference variable names, never actual values.
2. **Credentials are stored with restrictive permissions.** Config files use 0600 (owner read/write only).
3. **Tokens are short-lived.** Access tokens expire in 60 minutes. Refresh tokens expire in 7 days.
4. **Nothing sensitive goes over the wire unless encrypted.** All API communication uses HTTPS.

## Environment Variables

Tools declare which environment variables they need. You set the values. clictl injects them at execution time.

**Example tool spec:**

```yaml
# spec: "1.0"  (optional, defaults to "1.0")
name: open-meteo
auth:
  type: api_key
  key_env: GITHUB_TOKEN
  inject:
    location: query
    param: appid
```

**You set:**

```bash
export GITHUB_TOKEN=your-actual-key
```

**clictl does:** reads `GITHUB_TOKEN` from your environment, injects it into the request as the `appid` query parameter. The key never appears in the tool spec, skill file, or logs.

### Common patterns

| Auth type | Env var example | How it's injected |
|-----------|----------------|-------------------|
| API key (query) | `GITHUB_TOKEN` | `?appid=<value>` |
| API key (header) | `ANTHROPIC_API_KEY` | `x-api-key: <value>` |
| Bearer token | `GITHUB_TOKEN` | `Authorization: Bearer <value>` |
| Basic auth | `SERVICE_CREDENTIALS` | `Authorization: Basic <base64>` |

### What happens when a variable is missing

If a tool requires `GITHUB_TOKEN` and it's not set, clictl shows:

```
Error: open-meteo requires GITHUB_TOKEN
Set it with: export GITHUB_TOKEN=your-token
```

No fallback, no prompt. The variable must be present before execution.

## .env Files

clictl loads `.env` files automatically. This keeps secrets out of your shell history and makes them easy to manage per project.

**Load order (first found wins):**

1. `.env` in the current directory
2. `.env` in the project root (nearest parent with `.git`)
3. `~/.clictl/.env` (global defaults)

**Format:**

```env
# .env
GITHUB_TOKEN=abc123
GITHUB_TOKEN=ghp_xxxxxxxxxxxx
ANTHROPIC_API_KEY=sk-ant-xxxxxxxxxxxx
```

Rules:
- One variable per line, `KEY=VALUE` format
- Lines starting with `#` are comments
- No quotes needed around values (but single or double quotes are stripped if present)
- Empty lines are ignored
- Variables set in the shell environment take precedence over .env values

**Important:** Add `.env` to your `.gitignore`. clictl will warn if it detects a `.env` file that is tracked by Git.

## clictl Credentials

Your clictl account credentials (for registry access, publishing, etc.) are stored separately from tool secrets.

**Location:** `~/.clictl/config.yaml` (permissions: 0600)

```yaml
auth:
  api_key: "CLAK-..."              # personal API key
  access_token: "eyJ..."           # JWT access token (60 min)
  refresh_token: "eyJ..."          # JWT refresh token (7 days)
  expires_at: "2026-03-21T..."     # when access_token expires
```

**Token lifecycle:**

1. `clictl login` opens browser, completes OAuth, stores tokens
2. Access token is sent with API requests
3. When it expires, clictl uses the refresh token to get a new one automatically
4. If the refresh token expires, you need to `clictl login` again

**API key alternative:**

API keys can be passed as a flag to any command or set as an environment variable:

```bash
clictl search weather --api-key CLAK-your-key-here
# or
export CLICTL_API_KEY=CLAK-your-key-here
```

API keys don't expire (unless you revoke them). Create them at your workspace settings page.

**Precedence:** `--api-key` flag > `CLICTL_API_KEY` env var > config `api_key` > config `access_token`

## Memory Storage

Memories (notes your agent attaches to tools) are stored locally:

**Location:** `~/.clictl/memory/`

Memories are plain JSON files, one per tool. They contain no secrets, just text notes your agent has written. They are not synced, uploaded, or shared.

## Spec Cache

Cached tool specs are stored at `~/.clictl/cache/`. These are copies of publicly available spec files and contain no secrets. The cache uses ETag-based validation and can be cleared with:

```bash
rm -rf ~/.clictl/cache/
```

Or bypass it per-request:

```bash
clictl run open-meteo current --no-cache --q London
```

## Skill File Integrity

When installing skills that include `sha256` hashes in `source.files`, clictl verifies each downloaded file against the expected hash before writing it to disk.

**How hashes are computed:** The registry computes SHA256 hashes during `registry sync` by reading each file from the source URL and hashing its contents. Skill authors can also generate hashes locally using `clictl skill manifest <dir>`, which scans a directory and outputs a `files:` array with per-file SHA256 hashes ready to paste into a spec.

**What happens on mismatch:** If a downloaded file's SHA256 hash does not match the value in the spec, the install is aborted immediately with an error showing the expected and actual hashes. No files are written to disk for that skill.

**Missing hashes:** If a file in `source.files` has no `sha256` field, clictl prints a warning but continues the install. Skill authors should always include hashes for published specs.

## JavaScript Transform Sandbox

Tool specs can include JavaScript transforms that run via the goja engine. These are sandboxed:

- **No network access.** `fetch`, `XMLHttpRequest`, `WebSocket` are all blocked. A transform cannot call out to external services or exfiltrate credentials.
- **No code generation.** `eval` and the `Function` constructor are blocked. Scripts cannot dynamically generate and execute code.
- **No I/O.** No filesystem, no `console`, no `localStorage`. The script receives data in and returns data out.
- **No modules.** `require` and `import` are blocked.
- **5-second timeout.** Scripts that run too long are terminated.
- **Pre-execution validation.** Patterns like `new Function(` and `.constructor(` are rejected before the script even runs.

Safe operations (array methods, string manipulation, Math, JSON, object construction) all work normally.

## MCP Server Process Sandbox

When clictl spawns MCP server subprocesses (via `npx`, `uvx`, etc.), they run in a sandboxed environment to protect against supply chain attacks from compromised packages.

### Environment Scrubbing (all platforms)

Subprocess environment is built from an allowlist, not inherited from the parent:

- **Essential system vars:** `PATH`, `HOME`, `TMPDIR`, `LANG`, `USER`, `SHELL`, `TERM` (plus Windows equivalents)
- **Spec-declared vars:** Only env vars listed in `permissions.env` are passed through
- **Auth vars:** Only env vars configured in `auth[].key_env` are injected
- **Transport vars:** Literal values from `transport.env` in the spec
- **Marker:** `CLICTL_SANDBOX=1` is always set so processes can detect sandboxing

Everything else (AWS credentials, GitHub tokens, database URLs, etc.) is stripped unless the spec explicitly declares it.

### Filesystem Isolation

- **Linux (kernel 5.13+):** Landlock allowlist restricts filesystem access to declared paths only. Sensitive directories (`~/.ssh`, `~/.aws`, `~/.gnupg`, browser profiles, crypto wallets) are implicitly denied.
- **macOS:** `sandbox-exec` with a generated Scheme profile applies read/write restrictions.
- **Windows:** Job Objects contain the process tree (child processes cannot outlive the parent).

### Configuration

Sandbox is **on by default** and operates in **fail-closed** mode (`StrictSandbox` config option). If the sandbox cannot be initialized (e.g., missing platform support), execution is blocked rather than proceeding unsandboxed. To disable:

```yaml
# ~/.clictl/config.yaml
sandbox: false
```

Or per-invocation:

```bash
clictl mcp-serve --no-sandbox
```

Workspace admins on enterprise plans can enforce sandboxing via policy, preventing members from disabling it.

### Graceful Degradation

If the platform does not support the sandbox mechanism (e.g., Landlock on an older kernel, sandbox-exec with SIP disabled), clictl logs a warning and proceeds with env scrubbing only.

## Skill Isolation

Skills execute inside the AI agent's process, not as sandboxed subprocesses like MCP servers. This means a skill has the same capabilities as the agent itself unless explicitly restricted. Skill isolation addresses this by layering multiple restrictions on top of skill execution.

### Threat Model

When an agent loads a skill, the skill's instructions run with the agent's full tool access by default. A malicious or misconfigured skill could read sensitive files, execute arbitrary commands, or exfiltrate data through bash. Unlike MCP servers (which run in a separate process with OS-level sandboxing), skills require application-level isolation enforced by the agent and CLI together.

### Isolation Layers

Skill isolation uses a defense-in-depth approach with multiple independent layers:

**Layer 1: Tool Restriction (free)**
Skills declare which agent tools they need via `requires_tools` in the spec. During installation, this becomes the `allowed-tools` frontmatter in the generated SKILL.md. The agent only grants the skill access to listed tools. A skill that declares `requires_tools: [Bash, Read]` cannot use Write, Edit, or Grep.

**Layer 2: Filesystem Scope (free)**
Skills declare `permissions.filesystem.read` and `permissions.filesystem.write` paths. These are embedded in the generated skill file and enforced during execution. The skill can only read or write within the declared paths. Paths outside the scope are blocked.

**Layer 3: Bash Allowlisting (enterprise)**
Skills declare `bash_allow` patterns that restrict which shell commands the skill can run. Glob matching is supported (e.g., `clictl run *` allows any clictl run invocation). Commands not matching any pattern are rejected. This prevents arbitrary shell execution even when the Bash tool is available.

**Layer 4: Network Restriction (team+)**
The `permissions.network` list restricts which hosts the skill can communicate with. Outbound requests to unlisted hosts are blocked.

**Layer 5: Skill Signing (enterprise)**
Publishers sign skills with Ed25519 keys registered with the registry. During installation, clictl verifies the signature. Workspaces can require all installed skills to be signed, rejecting unsigned or tampered skills.

### Tier Availability

| Layer | Free | Team | Enterprise |
|-------|------|------|------------|
| Tool restriction | Yes | Yes | Yes |
| Filesystem scope | Yes | Yes | Yes |
| Network restriction | - | Yes | Yes |
| Bash allowlisting | - | - | Yes |
| Skill signing enforcement | - | - | Yes |
| Skill permission overrides | - | Yes | Yes |
| Managed skill sets | - | - | Yes |
| Skill audit logging | - | Yes | Yes |

### How Skills Complement the MCP Sandbox

The MCP process sandbox and skill isolation serve different purposes:

- **MCP sandbox** protects the host OS from a compromised MCP server subprocess. It uses OS-level mechanisms (Landlock, sandbox-exec, Job Objects) and environment scrubbing.
- **Skill isolation** protects the agent session from a malicious or overprivileged skill. It uses application-level restrictions (tool filtering, filesystem scope, bash allowlisting) enforced by the agent and CLI.

A tool that has both an MCP server and a skill component benefits from both layers. The MCP subprocess is sandboxed at the OS level, and the skill instructions are restricted at the application level.

## What Not to Do

- **Do not put secrets in tool spec YAML files.** Use `key_env` to reference environment variable names instead.
- **Do not commit `.env` files.** Add `.env` to `.gitignore`.
- **Do not share `~/.clictl/config.yaml`.** It contains your auth tokens.
- **Do not set secrets as default parameter values in specs.** Parameters are visible to everyone.
- **Do not use JavaScript transforms to handle secrets.** Transforms process response data only. Auth injection happens before the transform runs.

## Immutable Versions

Once a spec version is published, its content is locked. The registry rejects any attempt to overwrite an existing version. To make changes, bump the version number and publish again. This prevents supply-chain attacks where a published tool could be silently modified after users have installed it.

## Permission Declarations

Specs can declare what permissions a tool requires using the `permissions` field. This tells users and agents exactly what external access a tool needs before installation.

```yaml
permissions:
  network:
    - "api.github.com"
  env:
    - "GITHUB_TOKEN"
```

The `network` list declares which hosts the tool communicates with. The `env` list declares which environment variables the tool reads. These declarations are informational and displayed during `clictl install` and `clictl info` so users can make informed decisions.

## Reporting and Auto-Disable

Users can report tools that are broken, malicious, or otherwise problematic:

```bash
clictl report some-tool --reason "Returns malicious output"
```

Reports are sent to the registry. When a tool accumulates 3 or more reports, it is automatically disabled in the registry. Disabled tools cannot be installed or executed until a maintainer reviews and resolves the reports.

Use `clictl audit` to check your installed tools against the registry for any that have been reported or disabled.

## Tool Namespacing

Tool names use a `namespace/tool-name` format to prevent naming collisions across organizations. For example, `acme/deploy` and `bigcorp/deploy` can coexist without conflict. Namespaces use bare format with no `@` prefix.

## Package Runtime Safety

MCP server specs often point to packages hosted on npm or PyPI. Under the hood, clictl uses `npx` (Node) and `uvx` (Python) to run these packages. However, users never invoke `npx` or `uvx` directly. clictl manages the full lifecycle and adds several safety layers on top.

**Version pinning.** Every MCP spec pins the package to a specific version in its `package` field. clictl will not run a package at a floating or "latest" tag. This prevents surprise upgrades that could introduce malicious code.

```yaml
package:
  registry: npm
  name: "@org/mcp-server"
  version: "1.2.0"
```

**Package integrity.** When a spec includes SHA256 hashes, clictl verifies the downloaded package content before execution. A mismatch aborts the install immediately.

**Runtime detection.** Before installing an MCP package, clictl checks whether the required runtime (Node or Python) is available on the system. If the runtime is missing, the install fails with a clear message and a link to install instructions, rather than producing a confusing error from a missing binary.

**Permission declarations.** Specs declare what network hosts, environment variables, and filesystem paths the package needs access to. These are displayed during `clictl install` so users can review exactly what they are granting.

**The `--trust` flag.** Packages from unverified publishers require the `--trust` flag to install. Without it, clictl refuses to proceed. This forces an explicit opt-in for packages that have not been reviewed by the registry.

**What this means in practice:** instead of running `npx -y @some-org/unknown-package` and hoping for the best, clictl ensures the package is pinned, hashed, scoped to declared permissions, and explicitly trusted before any code executes.

## Reporting Vulnerabilities

If you find a security issue, email security@clictl.com. Do not open a public issue.

---

**See also:** [Securing Secrets](securing-secrets.md) | [CLI Reference](cli-reference.md) | [Spec Format](spec-format.md) | [Memory](memory.md)
