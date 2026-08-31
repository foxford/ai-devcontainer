#!/usr/bin/env bats
# Секция 7 wire-mcp.sh: codex_entry_kind() — определяет none/headers/other
# для секции [mcp_servers.<name>] в ~/.codex/config.toml. Мокаем сам codex
# (только нужен command -v codex -> true), реальная запись через мок-лог.

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
  # codex mcp add/remove — фиксируем вызовы, ничего реально не делаем.
  mock_bin codex

  export CODEX_HOME="$(mktemp -d)"
  export HERMES_HOME="$(mktemp -d)"
  export DSH_HOME="$(mktemp -d)/nonexistent"
}

teardown() {
  cleanup_fixture "$REPO_DIR"
  rm -f "$FIXTURE_SERVERS"
  mocks_cleanup
}

run_wire_mcp() {
  REPO_ROOT="$REPO_DIR" AI_DEVCONTAINER_MCP="$FIXTURE_SERVERS" run bash "$WIRE_MCP"
}

@test "нет config.toml — сервер с headers пробует обычное добавление (none)" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"figma": {"url": "https://example.com/mcp", "headers": {"Authorization": "Bearer x"}}}}
EOF
  run_wire_mcp
  assert_success
  # headers есть, config.toml нет секции -> "не добавляю, заголовки codex mcp add не выставляет"
  assert_output --partial "не добавляю — заголовки"
}

@test "секция уже с http_headers (вписана руками) — не трогаем, codex remove не зовём" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"figma": {"url": "https://example.com/mcp", "headers": {"Authorization": "Bearer x"}}}}
EOF
  cat > "$CODEX_HOME/config.toml" <<'EOF'
[mcp_servers.figma]
url = "https://example.com/mcp"

[mcp_servers.figma.http_headers]
Authorization = "Bearer manual"
EOF

  run_wire_mcp
  assert_success
  assert_output --partial "уже вписан руками"
  # codex mcp remove НЕ должен был вызваться для figma
  run cat "$MOCK_CALLS_DIR/codex.log"
  refute_output --partial "remove"
}

@test "секция без headers (other) — снимаем через codex mcp remove" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"figma": {"url": "https://example.com/mcp", "headers": {"Authorization": "Bearer x"}}}}
EOF
  cat > "$CODEX_HOME/config.toml" <<'EOF'
[mcp_servers.figma]
url = "https://example.com/mcp"
EOF

  run_wire_mcp
  assert_success
  assert_output --partial "снял запись"

  run cat "$MOCK_CALLS_DIR/codex.log"
  assert_output --partial "remove"
  assert_output --partial "figma"
}

@test "секция другого сервера с похожим именем не матчится (foo vs foobar)" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"foo": {"url": "https://example.com/mcp", "headers": {"Authorization": "Bearer x"}}}}
EOF
  cat > "$CODEX_HOME/config.toml" <<'EOF'
[mcp_servers.foobar]
url = "https://other.example.com/mcp"

[mcp_servers.foobar.http_headers]
Authorization = "Bearer manual"
EOF

  run_wire_mcp
  assert_success
  # foo не должен считаться "headers" из-за foobar — секции нет (none) ->
  # обычная ветка "не добавляю — заголовки codex mcp add не выставляет"
  assert_output --partial "не добавляю — заголовки"
  refute_output --partial "уже вписан руками"
}
