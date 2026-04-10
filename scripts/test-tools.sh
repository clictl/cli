#!/bin/bash
# Integration test for toolbox specs against live APIs.
# Run from cli/ directory after building: go build ./cmd/clictl/...
#
# Usage:
#   ./scripts/test-tools.sh              # Test all (skip auth tools without keys)
#   ./scripts/test-tools.sh --no-auth    # Test only no-auth tools
#   ./scripts/test-tools.sh --auth       # Test only auth tools (skip missing keys)
#   ./scripts/test-tools.sh --all        # Test everything (fail on missing keys)

set -euo pipefail

CLICTL="${CLICTL:-./clictl}"
PASS=0
FAIL=0
SKIP=0
TIMEOUT=30
MODE="${1:-all}"
FAILURES=""

test_tool() {
    local name="$1"
    shift
    if output=$(timeout "$TIMEOUT" $CLICTL run "$@" 2>/dev/null); then
        if [ -n "$output" ]; then
            printf "  \033[32mPASS\033[0m  %s\n" "$name"
            PASS=$((PASS + 1))
        else
            printf "  \033[31mFAIL\033[0m  %s (empty output)\n" "$name"
            FAIL=$((FAIL + 1))
            FAILURES="$FAILURES\n  $name (empty output)"
        fi
    else
        local code=$?
        if [ "$code" = "124" ]; then
            printf "  \033[31mFAIL\033[0m  %s (timeout after %ss)\n" "$name" "$TIMEOUT"
        else
            printf "  \033[31mFAIL\033[0m  %s (exit code %s)\n" "$name" "$code"
        fi
        FAIL=$((FAIL + 1))
        FAILURES="$FAILURES\n  $name (exit $code)"
    fi
}

test_auth_tool() {
    local name="$1"
    local key="$2"
    shift 2

    # Check if key is available (env var or vault)
    if [ -n "${!key:-}" ]; then
        test_tool "$name" "$@"
    elif $CLICTL vault get "$key" >/dev/null 2>&1; then
        test_tool "$name" "$@"
    else
        if [ "$MODE" = "--all" ]; then
            printf "  \033[31mFAIL\033[0m  %s (%s not set)\n" "$name" "$key"
            FAIL=$((FAIL + 1))
            FAILURES="$FAILURES\n  $name ($key not set)"
        else
            printf "  \033[33mSKIP\033[0m  %s (%s not set)\n" "$name" "$key"
            SKIP=$((SKIP + 1))
        fi
    fi
}

# Verify clictl exists
if [ ! -x "$CLICTL" ]; then
    echo "Error: $CLICTL not found. Run: go build ./cmd/clictl/..."
    exit 1
fi

echo "Testing toolbox specs against live APIs"
echo "Using: $CLICTL"
echo ""

# =========================================================================
# No-Auth REST APIs
# =========================================================================
if [ "$MODE" != "--auth" ]; then
echo "=== No-Auth REST APIs ==="
test_tool "nominatim" nominatim search --q "London"
test_tool "open-meteo" open-meteo current --latitude 51.5 --longitude -0.12
test_tool "httpbin" httpbin get --message "test"
test_tool "jsonplaceholder" jsonplaceholder posts
test_tool "dictionary" dictionary define --word "test"
test_tool "hackernews" hackernews top
test_tool "wikipedia" wikipedia search --query "rust"
test_tool "restcountries" restcountries all
test_tool "openlibrary" openlibrary search --q "dune"
test_tool "duckduckgo" duckduckgo search --q "test"
test_tool "stackoverflow" stackoverflow search --intitle "golang"
test_tool "crates-io" crates-io search --q "serde"
test_tool "pypi" pypi package-info --name "requests"
test_tool "npm-registry" npm-registry search --text "express"
test_tool "homebrew-formulae" homebrew-formulae search-formula --q "jq"
test_tool "cve" cve search --keywordSearch "log4j"
test_tool "data-gov" data-gov search_datasets --q "climate"
test_tool "worldbank" worldbank list-countries
test_tool "docker-hub" docker-hub search --q "nginx"
test_tool "archive-org" archive-org search --q "nasa"
test_tool "openstreetmap" openstreetmap search --q "Paris"
echo ""
fi

# =========================================================================
# Auth REST APIs
# =========================================================================
if [ "$MODE" != "--no-auth" ]; then
echo "=== Auth REST APIs ==="
test_auth_tool "github" GITHUB_TOKEN github repos --username octocat
test_auth_tool "github-graphql" GITHUB_TOKEN github-graphql my_repos
test_auth_tool "anthropic" ANTHROPIC_API_KEY anthropic models
test_auth_tool "openai" OPENAI_API_KEY openai models
test_auth_tool "groq" GROQ_API_KEY groq models
test_auth_tool "gitlab" GITLAB_TOKEN gitlab projects --search "gitlab"
test_auth_tool "deepl" DEEPL_API_KEY deepl usage
test_auth_tool "nasa" NASA_API_KEY nasa apod
test_auth_tool "tmdb" TMDB_TOKEN tmdb popular-movies
test_auth_tool "todoist" TODOIST_TOKEN todoist projects
test_auth_tool "ipinfo" IPINFO_TOKEN ipinfo lookup --ip "8.8.8.8"
test_auth_tool "coingecko" COINGECKO_API_KEY coingecko price --ids "bitcoin" --vs_currencies "usd"
test_auth_tool "notion" NOTION_TOKEN notion search --query "test"
test_auth_tool "sentry" SENTRY_AUTH_TOKEN sentry organizations
test_auth_tool "linear" LINEAR_API_KEY linear me
test_auth_tool "stripe" STRIPE_SECRET_KEY stripe balance
test_auth_tool "cloudflare" CLOUDFLARE_TOKEN cloudflare zones
test_auth_tool "vercel" VERCEL_TOKEN vercel projects
test_auth_tool "datadog" DD_API_KEY datadog monitors
test_auth_tool "slack" SLACK_TOKEN slack channels
test_auth_tool "shodan" SHODAN_API_KEY shodan host --ip "8.8.8.8"
test_auth_tool "perplexity" PERPLEXITY_API_KEY perplexity models
test_auth_tool "huggingface" HF_TOKEN huggingface models
test_auth_tool "unsplash" UNSPLASH_ACCESS_KEY unsplash search --query "sunset"
test_auth_tool "replicate" REPLICATE_API_TOKEN replicate models
test_auth_tool "sendgrid" SENDGRID_API_KEY sendgrid stats
test_auth_tool "discord" DISCORD_BOT_TOKEN discord guilds
test_auth_tool "uptimerobot" UPTIMEROBOT_API_KEY uptimerobot monitors
test_auth_tool "wolfram-alpha" WOLFRAM_APP_ID wolfram-alpha query --input "2+2"
test_auth_tool "news-api" NEWS_API_KEY news-api headlines --country "us"
test_auth_tool "virustotal" VT_API_KEY virustotal domain --domain "google.com"
echo ""
fi

# =========================================================================
# Results
# =========================================================================
echo "==========================================="
echo "  Results"
echo "==========================================="
printf "  \033[32mPASS: %d\033[0m\n" "$PASS"
printf "  \033[31mFAIL: %d\033[0m\n" "$FAIL"
printf "  \033[33mSKIP: %d\033[0m\n" "$SKIP"
echo "  Total: $((PASS + FAIL + SKIP))"

if [ "$FAIL" -gt 0 ]; then
    echo ""
    echo "Failures:"
    printf "$FAILURES\n"
    exit 1
fi
