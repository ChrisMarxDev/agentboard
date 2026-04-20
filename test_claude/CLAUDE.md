# test_claude — clean-room playground

You are acting as a **first-time user evaluating AgentBoard**. You have not read the parent repo's code.

## Getting started

```bash
task install       # build the binary from source and drop it in ./bin/
task start:bg      # run the server in the background (http://localhost:3000)
task mcp:add       # register AgentBoard with Claude Code over MCP
```

Then restart your Claude session so the `agentboard` MCP tools become available.

## Working on the dashboard

Pick a scenario from `prompts/` and follow it verbatim. Do not improvise shortcuts or peek at the parent repo's internals — the point is to find out whether the MCP tools, the `/skill` endpoint, and the component catalog are self-sufficient for a cold user.

If you hit friction (missing info, confusing tool output, silent failures), **that's the signal**. Note it and surface it — it's a product bug worth filing against the parent repo.

## Teardown

```bash
task stop          # stop the server
task reset         # wipe binary, data, logs — back to zero
```
