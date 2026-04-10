# Security Policy

## Reporting Vulnerabilities

If you discover a security vulnerability in clictl, please report it privately. Do not open a public issue.

**Email:** security@clictl.com

We will acknowledge your report within 48 hours and provide a timeline for a fix. We follow responsible disclosure practices. If you want, we will credit you in the advisory.

## How clictl Handles Secrets

### Tool credentials

Tool specs never contain actual secrets. They reference environment variable names:

```yaml
auth:
  type: api_key
  key_env: OWM_API_KEY       # name of the variable, not the value
```

Users set the actual values via environment variables or `.env` files. clictl reads them at execution time and injects them into requests. Secrets never appear in spec files, skill files, logs, or Git history.

### .env file support

clictl loads `.env` files from the current directory, project root, or `~/.clictl/.env`. Variables set in the shell take precedence over .env values.

Add `.env` to `.gitignore`. clictl warns if a `.env` file is tracked by Git.

### User credentials

OAuth tokens and API keys are stored at `~/.clictl/config.yaml` with 0600 permissions (owner read/write only). Access tokens expire in 60 minutes. Refresh tokens expire in 7 days.

### Platform (backend)

- All API communication uses HTTPS
- Passwords are hashed (never stored in plaintext)
- JWT tokens include a session_id claim for instant revocation
- Personal API keys are stored as SHA-256 hashes
- OAuth state and codes are stored in Redis with 600-second TTL
- Email verification tokens expire in 24 hours
- SCIM tokens are stored as SHA-256 hashes
- SAML assertions are validated server-side

### Data at rest

- Tool connection tokens (OAuth for third-party APIs) are encrypted
- Workspace SCIM tokens and invitation tokens are stored as SHA-256 hashes
- No plaintext secrets in the database

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest | Yes |
| Previous minor | Security fixes only |
| Older | No |

## Scope

The following are in scope for security reports:

- Authentication bypass
- Authorization flaws (accessing another user's data)
- Credential exposure (tokens, keys, passwords)
- Injection vulnerabilities (SQL, command, XSS)
- CSRF on state-changing endpoints
- Insecure token storage or transmission

The following are out of scope:

- Rate limiting or denial of service
- Social engineering
- Vulnerabilities in dependencies (report to the upstream project)
- Issues requiring physical access to the machine
