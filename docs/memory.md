# Memory

Agents forget what they learn between sessions. Memory fixes that. When your agent figures out a workaround, discovers a rate limit, or finds the right parameters for a tool, it can save that knowledge for next time.

## How It Works

Memories are local notes attached to tools. They persist across sessions and appear automatically when the agent inspects or uses a tool.

```bash
clictl remember open-meteo "use --units metric for EU users"
```

Next time the agent runs `clictl info open-meteo`, the memory appears:

```
open-meteo v2.5
  Weather data and forecasts

  Actions:
    current    Get current weather conditions
    forecast   Get 5-day forecast

  Memories:
    - use --units metric for EU users (2026-03-20)
```

Memories also appear in skill files (`.claude/skills/<tool>/SKILL.md`), so agents that read skill files get the same benefit.

## Commands

### remember

Attach a note to a tool.

```bash
clictl remember open-meteo "use --units metric for EU users"
clictl remember github "rate limit is 5000/hr with token, 60/hr without"
clictl remember slack "rate limit resets every 60s, batch messages"
clictl remember anthropic "use max_tokens=4096 for longer responses"
```

Each call adds a new memory. A tool can have multiple memories.

### memory

View memories for a tool.

```bash
clictl memory open-meteo
# Memories for open-meteo:
#   1. use --units metric for EU users (2026-03-20)

clictl memory --all
# Tools with memories:
#   open-meteo (1 memory)
#   github (2 memories)
#   slack (1 memory)
```

### forget

Remove memories.

```bash
clictl forget open-meteo           # interactive: pick which to remove
clictl forget open-meteo --all     # remove all memories for a tool
```

## What to Remember

Good memories capture things that aren't obvious from the tool spec:

- **Rate limits:** "rate limit is 60 requests/minute, batch calls"
- **Auth gotchas:** "needs GITHUB_TOKEN for private repos, not just public"
- **Parameter tips:** "use --units metric for EU, --units imperial for US"
- **Error workarounds:** "returns 500 if query contains special characters, URL-encode first"
- **Version notes:** "v2 endpoint is faster but requires different auth"
- **Project context:** "we use the forecast action for the daily digest feature"

Don't remember things already stated in the tool spec (like what parameters are required).

## How Agents Use It

Prompt your agent to use memory:

> "Use `clictl remember` to save anything useful you learn about tools. Check `clictl memory` before using a tool you've used before."

Or create a skill/rule that includes this behavior automatically.

**The loop:**

1. Agent uses a tool, encounters something unexpected
2. Agent runs `clictl remember <tool> "what it learned"`
3. Next session, agent runs `clictl info <tool>` or reads the skill file
4. Memory appears, agent avoids the same issue
5. Agent discovers more, adds more memories
6. Over time, the agent gets better with every tool it uses

## Storage

Memories are stored at `~/.clictl/memory/` as JSON files, one per tool:

```
~/.clictl/memory/
  open-meteo.json
  github.json
  slack.json
```

Each file is an array of timestamped notes:

```json
[
  {
    "note": "use --units metric for EU users",
    "created_at": "2026-03-20T10:30:00Z"
  }
]
```

Memories are local to your machine. They are not synced, uploaded, or shared with anyone. They exist solely to make your agent smarter over time.

---

**See also:** [CLI Reference](cli-reference.md) | [Spec Format](spec-format.md) | [Security](security.md)
