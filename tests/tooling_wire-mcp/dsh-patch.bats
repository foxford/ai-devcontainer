#!/usr/bin/env bats
# Секция 9 wire-mcp.sh: генерация ~/.dsh/cordis.patch.yml. Мокаем dsh (только
# нужен command -v dsh -> true; сам dsh не вызывается для генерации, только
# как gate). Проверяем итоговый файл-патч.

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
  mock_bin dsh

  export CODEX_HOME="$(mktemp -d)/nonexistent"
  export HERMES_HOME="$(mktemp -d)/nonexistent"
  export DSH_HOME="$(mktemp -d)"
  DSH_PATCH="$DSH_HOME/cordis.patch.yml"
}

teardown() {
  cleanup_fixture "$REPO_DIR"
  rm -f "$FIXTURE_SERVERS"
  mocks_cleanup
}

run_wire_mcp() {
  REPO_ROOT="$REPO_DIR" AI_DEVCONTAINER_MCP="$FIXTURE_SERVERS" run bash "$WIRE_MCP"
}

@test "stdio-сервер — корректный блок с command/args/env/cwd" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"playwright": {"command": "npx", "args": ["-y", "@playwright/mcp"], "env": {"FOO": "bar"}}}}
EOF
  run_wire_mcp
  assert_success

  run cat "$DSH_PATCH"
  assert_output --partial '- insert:'
  assert_output --partial 'id: "ai-devcontainer-mcp-playwright"'
  assert_output --partial 'name: "@deepseek-ai/dsh-mcp-client"'
  assert_output --partial 'serverName: "playwright"'
  assert_output --partial 'transport: "stdio"'
  assert_output --partial 'command: "npx"'
  assert_output --partial '"FOO"'
  assert_output --partial '"bar"'
  assert_output --partial "cwd: \"$REPO_DIR\""
}

@test "url-сервер — transport streamable-http + headers" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"figma": {"url": "https://example.com/mcp", "headers": {"Authorization": "Bearer x"}, "x-requires": []}}}
EOF
  run_wire_mcp
  assert_success

  run cat "$DSH_PATCH"
  assert_output --partial 'transport: "streamable-http"'
  assert_output --partial 'url: "https://example.com/mcp"'
  assert_output --partial '"Authorization"'
  assert_output --partial '"Bearer x"'
}

@test "невалидное имя сервера (спецсимволы) — не попадает в патч" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"bad!name": {"command": "echo"}}}
EOF
  run_wire_mcp
  assert_success

  run cat "$DSH_PATCH"
  refute_output --partial 'bad!name'
}

@test "OAuth-сервер пропускается" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"figma": {"url": "https://example.com/mcp", "x-oauth": true}}}
EOF
  run_wire_mcp
  assert_success

  run cat "$DSH_PATCH"
  refute_output --partial 'figma'
}

@test "пустой список серверов — блок не пишется" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {}}
EOF
  run_wire_mcp
  assert_success
  [ ! -s "$DSH_PATCH" ] || {
    run cat "$DSH_PATCH"
    refute_output --partial '- insert:'
  }
}

@test "плейсхолдер [] заменяется, а не ломает документ" {
  echo "[]" > "$DSH_PATCH"
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"playwright": {"command": "npx", "args": ["-y"]}}}
EOF
  run_wire_mcp
  assert_success

  run cat "$DSH_PATCH"
  assert_output --partial '- insert:'
  # плейсхолдер "[]" как ОТДЕЛЬНАЯ строка не должен остаться (в отличие от
  # легитимного "args: [...]" где-то внутри блока).
  refute_line '[]'
}

@test "чужой контент вокруг маркеров сохраняется, повторный прогон не дублирует блок" {
  cat > "$DSH_PATCH" <<'EOF'
# моя ручная правка сверху
- custom: true
EOF
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"playwright": {"command": "npx", "args": []}}}
EOF
  run_wire_mcp
  assert_success
  run_wire_mcp
  assert_success

  run cat "$DSH_PATCH"
  assert_output --partial 'моя ручная правка сверху'
  assert_output --partial 'custom: true'
  # ровно один блок insert для playwright, не два
  count="$(grep -c 'id: "ai-devcontainer-mcp-playwright"' "$DSH_PATCH")"
  [ "$count" -eq 1 ]
}
