#!/usr/bin/env bats
# Секция 2 wire-mcp.sh: парсинг .agents/mcp.secrets.env + merge с окружением.
# Тестируем end-to-end через реальный запуск wire-mcp.sh (контракт, не
# implementation detail) — эти тесты должны остаться зелёными и после
# переписывания python-инлайна на bash/jq.

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/fixtures'

  PLATFORM_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  WIRE_MCP="$PLATFORM_ROOT/tooling/wire-mcp.sh"

  REPO_DIR="$(make_repo_fixture)"
  mkdir -p "$REPO_DIR/.agents"
  FIXTURE_SERVERS="$(mktemp)"

  # Изолируем Codex/Hermes/DSH секции: указываем на заведомо пустые HOME,
  # ни codex/yq/dsh не подсовываем в PATH — эти секции сами себя пропустят.
  export CODEX_HOME="$(mktemp -d)"
  export HERMES_HOME="$(mktemp -d)"
  export DSH_HOME="$(mktemp -d)/nonexistent"
}

teardown() {
  cleanup_fixture "$REPO_DIR"
  rm -f "$FIXTURE_SERVERS"
}

run_wire_mcp() {
  REPO_ROOT="$REPO_DIR" AI_DEVCONTAINER_MCP="$FIXTURE_SERVERS" run bash "$WIRE_MCP"
}

@test "секрет из mcp.secrets.env раскрывается в .mcp.json" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"echo": {"command": "echo", "args": ["${TEST_SECRET_VAR}"]}}}
EOF
  printf 'TEST_SECRET_VAR=hello world\n' > "$REPO_DIR/.agents/mcp.secrets.env"

  run_wire_mcp
  assert_success

  run cat "$REPO_DIR/.mcp.json"
  assert_output --partial '"hello world"'
}

@test "значение в кавычках — кавычки снимаются" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"echo": {"command": "echo", "args": ["${TEST_SECRET_VAR}"]}}}
EOF
  printf 'TEST_SECRET_VAR="quoted value"\n' > "$REPO_DIR/.agents/mcp.secrets.env"

  run_wire_mcp
  assert_success

  run cat "$REPO_DIR/.mcp.json"
  assert_output --partial '"quoted value"'
  refute_output --partial '"\"quoted value\""'
}

@test "export-префикс снимается" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"echo": {"command": "echo", "args": ["${TEST_SECRET_VAR}"]}}}
EOF
  printf 'export TEST_SECRET_VAR=exported\n' > "$REPO_DIR/.agents/mcp.secrets.env"

  run_wire_mcp
  assert_success

  run cat "$REPO_DIR/.mcp.json"
  assert_output --partial '"exported"'
}

@test "комментарии и пустые строки игнорируются" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"echo": {"command": "echo", "args": ["${TEST_SECRET_VAR}"]}}}
EOF
  printf '# comment\n\nTEST_SECRET_VAR=value\n' > "$REPO_DIR/.agents/mcp.secrets.env"

  run_wire_mcp
  assert_success

  run cat "$REPO_DIR/.mcp.json"
  assert_output --partial '"value"'
}

@test "дубль ключа — последняя строка побеждает, предупреждение в stderr" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"echo": {"command": "echo", "args": ["${TEST_SECRET_VAR}"]}}}
EOF
  printf 'TEST_SECRET_VAR=first\nTEST_SECRET_VAR=second\n' > "$REPO_DIR/.agents/mcp.secrets.env"

  run_wire_mcp
  assert_success

  run cat "$REPO_DIR/.mcp.json"
  assert_output --partial '"second"'
  refute_output --partial '"first"'
}

@test "REPO_ROOT главнее secrets-файла и окружения" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"echo": {"command": "echo", "args": ["${REPO_ROOT}"]}}}
EOF
  printf 'REPO_ROOT=/should/not/win\n' > "$REPO_DIR/.agents/mcp.secrets.env"

  run_wire_mcp
  assert_success

  run cat "$REPO_DIR/.mcp.json"
  assert_output --partial "$REPO_DIR"
  refute_output --partial "/should/not/win"
}

@test "ключ с недопустимыми символами игнорируется" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"echo": {"command": "echo", "args": ["${VALID_VAR}"]}}}
EOF
  printf '1INVALID=oops\nVALID_VAR=ok\n' > "$REPO_DIR/.agents/mcp.secrets.env"

  run_wire_mcp
  assert_success

  run cat "$REPO_DIR/.mcp.json"
  assert_output --partial '"ok"'
}

@test "нераскрытая подстановка остаётся текстом и предупреждает" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"echo": {"command": "echo", "args": ["${TOTALLY_UNKNOWN_VAR}"]}}}
EOF
  run_wire_mcp
  assert_success
  assert_output --partial 'нераскрытые подстановки'

  run cat "$REPO_DIR/.mcp.json"
  assert_output --partial '${TOTALLY_UNKNOWN_VAR}'
}
