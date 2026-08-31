#!/usr/bin/env bash
# Shell hook: pre_tool_call validation for kanban_complete and kanban_create.
#
# Forwards the JSON payload from Hermes to the state machine's HTTP server,
# which validates against Zod schemas in pipeline.ts. The server returns
# either {} (allow) or {"action": "block", "message": "..."} (block).
#
# Configured in ~/.hermes/config.yaml hooks: section by bootstrap.sh.
set -e
PORT="${HERMES_SM_PORT:-43210}"
TIMEOUT=3

payload="$(cat -)"

# Try the validation server. If unreachable (server down), fail-open
# rather than blocking the agent.
response=$(curl -sS --max-time $TIMEOUT \
  -X POST "http://127.0.0.1:${PORT}/validate" \
  -H "Content-Type: application/json" \
  --data-binary "$payload" 2>/dev/null || echo '{}')

# Echo response back to Hermes (it parses for action=="block")
echo "$response"
