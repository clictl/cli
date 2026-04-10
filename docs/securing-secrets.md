# Securing Secrets

This guide covers the clictl vault and the `vault://` protocol for managing secrets safely across tool workflows.

## The problem

Most developer workflows store API keys and tokens in one of these places:

- `.env` files that risk being committed to git
- Environment variables visible in `ps aux`, `/proc/PID/environ`, and CI logs
- Shell history (`export STRIPE_KEY=sk_live_...`)
- Plaintext config files

Any of these can leak secrets to teammates, CI systems, log aggregators, or attackers who gain read access to the filesystem.

## The `vault://` protocol

The `vault://` protocol replaces plaintext secrets with encrypted references. Instead of storing the actual value, you store a pointer:

```bash
# Before (insecure):
STRIPE_API_KEY=sk_live_abc123

# After (secure):
STRIPE_API_KEY=vault://STRIPE_API_KEY
```

When clictl executes a tool, it sees the `vault://` prefix, decrypts the real value from the vault, and passes it to the tool. The actual secret exists only in memory during execution.

## Storing secrets with `clictl vault set`

```bash
clictl vault set STRIPE_API_KEY sk_live_abc123
clictl vault set GITHUB_TOKEN ghp_def456
```

After running `vault set`, clictl:

1. Encrypts the value and stores it in `~/.clictl/vault.enc`
2. If a `.env` file exists in the current directory with a matching key, updates its value to `vault://STRIPE_API_KEY`
3. If no `.env` entry exists, prints the `vault://` reference for you to add manually

The vault file is encrypted and protected with 0600 permissions (owner read/write only).

## Migrating existing `.env` files with `clictl vault import`

If you already have a `.env` file with plaintext secrets:

```bash
clictl vault import .env
```

This reads every key-value pair, stores the values in the vault, and replaces the plaintext values in the file with `vault://` references. Comments and blank lines are preserved.

To skip non-secret values:

```bash
clictl vault import .env --exclude NODE_ENV,DEBUG,PORT
```

Values that are already `vault://` references are skipped automatically.

## Per-project vaulting with `--project`

By default, secrets go to the user vault at `~/.clictl/vault.enc`. For secrets specific to a single project, use the `--project` flag:

```bash
clictl vault set DATABASE_URL postgres://user:pass@host/db --project
```

This stores the secret in `.clictl/vault.enc` relative to the current git root. The CLI auto-adds this path to `.gitignore`.

**Resolution order** (first match wins):

1. Project vault (`.clictl/vault.enc` in the current git root)
2. User vault (`~/.clictl/vault.enc`)
3. Workspace vault (enterprise, via API with caching)
4. Raw environment variable value

This means you can have project-specific overrides that take precedence over your global secrets.

## Workspace secrets (enterprise)

Enterprise workspaces can manage secrets centrally. Admins set secrets via the web UI or API, and team members' CLIs resolve them automatically.

**Admin sets a shared secret:**

In the web UI, go to Settings > Secrets and click "Add Secret". Provide the name, value, and cache TTL.

**Developer uses the secret:**

Add a `vault://` reference to your `.env`:

```
STRIPE_KEY=vault://STRIPE_KEY
```

The CLI resolves workspace secrets with server-driven caching:

- First resolve: the CLI fetches the secret from the API, caches it locally (encrypted), and respects the `Cache-Control` header for freshness
- Within the cache window: the CLI reads from the local cache with no API call
- After cache expiry: the CLI revalidates with an `ETag`. If the secret has not changed, the server responds with 304 (no payload) and the cache is extended

Admins control the cache TTL per secret. Shorter TTLs mean faster rotation propagation but more API calls.

## Managing vault contents

```bash
# List secret names and timestamps (never shows values)
clictl vault list

# Retrieve a value (requires confirmation prompt)
clictl vault get STRIPE_API_KEY

# Delete a secret
clictl vault delete STRIPE_API_KEY

# Update a secret (same as set - overwrites the existing value)
clictl vault set STRIPE_API_KEY sk_live_newkey789

# Export all secrets as plaintext (for CI migration, requires --confirm)
clictl vault export --format env --confirm
```

## Vault initialization

The vault key is generated automatically during `clictl install` (self-install). No extra setup is needed.

**Reset the vault** (destroys all existing secrets):

```bash
clictl vault init --force
```

**Use a password-based key** (for portability across machines):

```bash
clictl vault init --password
```

This derives the encryption key from a password you enter instead of random bytes. The password is never stored. You enter it once per session on first vault access. This lets you use the same vault file across multiple machines.

## Attack scenarios

| Attack | Without vault:// | With vault:// |
|--------|-----------------|---------------|
| `.env` committed to git | Secrets leaked in repository | Only `vault://` references leaked |
| `ps aux` or process listing | Secrets visible in environment | Only references visible |
| CI/CD log leak (env dump) | Secrets appear in logs | Only references in logs |
| Shoulder surfing terminal | `export KEY=secret` visible | `export KEY=vault://KEY` visible |
| Malicious tool reads env | Gets actual secrets | Gets `vault://` reference (useless without vault file) |
| Stolen laptop (disk access) | `.env` files are readable | Vault file is encrypted, requires key or password |
| MCP server prompt injection | Tool dumps env vars with real values | Env vars only contain references |

## What `vault://` does NOT protect against

- **Memory dump during tool execution.** The secret is in memory briefly while the tool runs.
- **Compromised CLI binary.** If an attacker modifies the clictl binary, they can intercept secrets during resolution.
- **Stolen vault file and vault key together.** Both are needed to decrypt, but if an attacker has both, secrets are exposed.
- **Root access on the user's machine.** A root-level attacker can read anything in memory or on disk.

The vault is not a replacement for infrastructure-level secret management in production environments. It is designed to protect developer workstations and CI pipelines from common secret exposure patterns.

---

**See also:** [Security Model](security.md) | [CLI Reference](cli-reference.md) | [Spec Format](spec-format.md)
