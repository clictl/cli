# Contributing

Thanks for your interest in contributing to clictl. This guide covers the basics.

## Quick Start

```bash
git clone https://github.com/clictl/cli.git
cd cli
make build
make test
```

Requires Go 1.23+.

## What to Contribute

**Good first contributions:**

- Bug fixes with a failing test
- New protocol support (GraphQL, gRPC, WebSocket)
- Improvements to error messages
- Documentation fixes

**Before starting large changes:**

Open an issue first to discuss the approach. This saves time for everyone.

## Development Workflow

1. Fork the repo
2. Create a branch from `main`
3. Make your changes
4. Run `make test` and `go vet ./...`
5. Commit with a clear message
6. Open a pull request

## Code Style

- Standard `gofmt` formatting (enforced by CI)
- Explicit `if err != nil` error handling, no panics in production paths
- Context (`ctx`) as the first argument in network/long-running functions
- Error wrapping: `fmt.Errorf("doing X: %w", err)`
- All exported types and functions must have godoc comments

## Tests

Every new feature or bug fix should include tests.

```bash
make test                    # run all tests
go test ./internal/...       # run specific package tests
```

- Test files live next to source: `foo.go` -> `foo_test.go`
- Use `t.TempDir()` for file-based tests
- Use `t.Setenv()` for environment variables
- Use `httptest.NewServer` for HTTP client tests

## Commit Messages

Keep them short and descriptive. Use the imperative mood.

```
fix: handle missing API key error gracefully
feat: add GraphQL protocol support
docs: update CLI reference for memory commands
test: add exec command parameter validation tests
```

## Pull Requests

- One feature or fix per PR
- Include tests for new behavior
- Update docs if you change commands, flags, or config
- Keep the diff focused. Don't refactor unrelated code in the same PR.

## Project Structure

```
cmd/clictl/          Entry point
internal/
  command/           Cobra CLI commands
  config/            Config loading, auth resolution
  executor/          Protocol-specific execution (HTTP)
  httpcache/         RFC 7234 response cache
  mcp/               MCP stdio server
  models/            Data structures
  registry/          API client, cache, index, spec resolution
  updater/           Auto-update, version check, registry sync
```

## Skill Signing

When publishing skills to a toolbox, you can sign them for provenance verification. Signed skills allow workspaces to enforce that only trusted publishers' skills are installed.

### Signing Workflow

1. **Generate a keypair** (one-time setup):

```bash
clictl skill keygen --output ~/.clictl/signing-key
```

This creates `~/.clictl/signing-key` (private) and `~/.clictl/signing-key.pub` (public). Register the public key with the registry via your workspace settings.

2. **Generate the manifest with hashes:**

```bash
clictl skill manifest ./my-skill/
```

This outputs the `source.files` array with SHA256 hashes for each file.

3. **Sign the skill:**

```bash
clictl skill sign ./my-skill/ --key ~/.clictl/signing-key
```

This computes a signature over all files and their hashes, then outputs the `source.signature` value to include in your spec.

4. **Publish the spec** with the signature field included.

### Testing Skill Isolation Locally

You can test how skill isolation behaves without publishing to a toolbox:

1. **Install a local skill with isolation fields:**

```bash
# Create a test spec with requires_tools, permissions.filesystem, and bash_allow
clictl install ./test-spec.yaml --target ./test-project
```

2. **Verify the generated SKILL.md** contains the expected isolation metadata:

```bash
cat ./test-project/.claude/skills/test-tool/SKILL.md
```

Check that `allowed-tools` in the frontmatter matches `requires_tools`, filesystem scope comments are present, and bash-allow comments match `bash_allow` patterns.

3. **Test tool restriction** by invoking the skill in your agent and verifying that tools outside `allowed-tools` are not available.

4. **Test filesystem scope** by confirming the skill cannot read or write outside the declared paths.

5. **Test bash allowlisting** by confirming that commands not matching any `bash_allow` pattern are rejected.

## Questions

Open an issue on GitHub. For security issues, see [SECURITY.md](SECURITY.md).
