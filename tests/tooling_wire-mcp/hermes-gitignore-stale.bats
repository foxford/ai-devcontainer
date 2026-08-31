#!/usr/bin/env bats
# Секции 8, 10, 11 wire-mcp.sh: Hermes config.yaml, stale-cleanup, .gitignore.

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/fixtures'
  load '../helpers/mocks'

  PLATFORM_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  WIRE_MCP="$PLATFORM_ROOT/tooling/wire-mcp.sh"

  REPO_DIR="$(make_repo_fixture)"
  mkdir -p "$REPO_DIR/.agents"
  FIXTURE_SERVERS="$(mktemp)"

  mocks_init
  export CODEX_HOME="$(mktemp -d)/nonexistent"
  export DSH_HOME="$(mktemp -d)/nonexistent"
  export HERMES_HOME="$(mktemp -d)"
}

teardown() {
  cleanup_fixture "$REPO_DIR"
  rm -f "$FIXTURE_SERVERS"
  mocks_cleanup
}

run_wire_mcp() {
  REPO_ROOT="$REPO_DIR" AI_DEVCONTAINER_MCP="$FIXTURE_SERVERS" run bash "$WIRE_MCP"
}

# ── Hermes ────────────────────────────────────────────────────
# yq (kislyuk/yq, python-based) реальный, если стоит; иначе секция сама себя
# пропускает — это штатное поведение кода, не требующее мока.

@test "Hermes: сервер записывается в mcp_servers, если yq доступен" {
  if ! command -v yq >/dev/null 2>&1; then skip "нет yq в этом окружении"; fi
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"echo": {"command": "echo", "args": []}}}
EOF
  run_wire_mcp
  assert_success
  run cat "$HERMES_HOME/config.yaml"
  assert_output --partial "echo"
}

@test "Hermes: нет yq — секция сама себя пропускает" {
  # Урезанный PATH без yq, но с нужными coreutils/jq/git для остального скрипта.
  local minimal_path
  minimal_path="$(dirname "$(command -v jq)"):$(dirname "$(command -v git)"):/usr/bin:/bin"
  if command -v yq >/dev/null 2>&1 && [[ ":$minimal_path:" == *":$(dirname "$(command -v yq)"):"* ]]; then
    skip "yq стоит в том же каталоге, что jq/git/coreutils — не изолировать"
  fi
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"echo": {"command": "echo", "args": []}}}
EOF
  PATH="$minimal_path" run_wire_mcp
  assert_success
  assert_output --partial "нет yq"
}

# ── stale cleanup ─────────────────────────────────────────────

@test "сервер, убранный из конфига, чистится из Claude .mcp.json" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"one": {"command": "echo"}, "two": {"command": "echo"}}}
EOF
  run_wire_mcp
  assert_success
  run cat "$REPO_DIR/.mcp.json"
  assert_output --partial '"one"'
  assert_output --partial '"two"'

  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"one": {"command": "echo"}}}
EOF
  run_wire_mcp
  assert_success
  assert_output --partial "убрано: two"

  run cat "$REPO_DIR/.mcp.json"
  assert_output --partial '"one"'
  refute_output --partial '"two"'
}

@test "пустой набор серверов — .mcp.json удаляется" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"one": {"command": "echo"}}}
EOF
  run_wire_mcp
  assert_success
  [ -f "$REPO_DIR/.mcp.json" ]

  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {}}
EOF
  run_wire_mcp
  assert_success
  [ ! -f "$REPO_DIR/.mcp.json" ]
}

@test ".mcp.json, раскладываемый не нами (нет state-файла), не трогаем" {
  echo '{"mcpServers": {"manual": {"command": "custom"}}}' > "$REPO_DIR/.mcp.json"
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"one": {"command": "echo"}}}
EOF
  run_wire_mcp
  assert_success
  assert_output --partial "раскладывали его не мы"

  run cat "$REPO_DIR/.mcp.json"
  assert_output --partial '"manual"'
  refute_output --partial '"one"'
}

# ── .gitignore ────────────────────────────────────────────────

@test ".gitignore: дописывает недостающие строки один раз" {
  echo "node_modules/" > "$REPO_DIR/.gitignore"
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {}}
EOF
  run_wire_mcp
  assert_success
  run cat "$REPO_DIR/.gitignore"
  assert_output --partial "/.mcp.json"
  assert_output --partial "/.agents/mcp.secrets.env"

  run_wire_mcp
  assert_success
  run bash -c "grep -c '/.mcp.json' '$REPO_DIR/.gitignore'"
  assert_output "1"
}
