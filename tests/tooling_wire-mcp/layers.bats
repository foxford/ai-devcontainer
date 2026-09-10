#!/usr/bin/env bats
# Секции 3-4a wire-mcp.sh: четыре слоя, их приоритет и режим --dump-plan.
#
# Приоритет проверяем именно end-to-end через .mcp.json и --dump-plan: правило
# «проектное бьёт глобальное, моё бьёт общее» — это контракт, на который
# опирается человек, а не деталь реализации мерджа.

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/fixtures'

  PLATFORM_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  WIRE_MCP="$PLATFORM_ROOT/tooling/wire-mcp.sh"

  REPO_DIR="$(make_repo_fixture)"
  mkdir -p "$REPO_DIR/.agents"
  FIXTURE_SERVERS="$(mktemp)"
  USER_STORE="$(mktemp -d)"

  export CODEX_HOME="$(mktemp -d)"
  export HERMES_HOME="$(mktemp -d)"
  export DSH_HOME="$(mktemp -d)/nonexistent"
}

teardown() {
  cleanup_fixture "$REPO_DIR"
  rm -f "$FIXTURE_SERVERS"
  rm -rf "$USER_STORE"
}

wire() {
  REPO_ROOT="$REPO_DIR" \
  AI_DEVCONTAINER_MCP="$FIXTURE_SERVERS" \
  AI_DEVCONTAINER_MCP_STORE="$USER_STORE" \
  run bash "$WIRE_MCP" "$@"
}

# Слой платформы: один сервер `srv` с меткой, по которой видно, чей он.
seed_global() {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"srv": {"command": "echo", "args": ["global"]}}}
EOF
}

@test "четыре слоя: локальный перекрывает проектный, проектный — пользовательский" {
  seed_global
  echo '{"mcpServers":{"srv":{"command":"echo","args":["user"]}}}'    > "$USER_STORE/servers.json"
  echo '{"mcpServers":{"srv":{"command":"echo","args":["project"]}}}' > "$REPO_DIR/.agents/mcp.json"
  echo '{"mcpServers":{"srv":{"command":"echo","args":["local"]}}}'   > "$REPO_DIR/.agents/mcp.local.json"

  wire
  assert_success
  run jq -r '.mcpServers.srv.args[0]' "$REPO_DIR/.mcp.json"
  assert_output "local"
}

@test "пользовательский слой перекрывает платформенный, но уступает проектному" {
  seed_global
  echo '{"mcpServers":{"srv":{"command":"echo","args":["user"]}}}' > "$USER_STORE/servers.json"

  wire
  run jq -r '.mcpServers.srv.args[0]' "$REPO_DIR/.mcp.json"
  assert_output "user"

  echo '{"mcpServers":{"srv":{"command":"echo","args":["project"]}}}' > "$REPO_DIR/.agents/mcp.json"
  wire
  run jq -r '.mcpServers.srv.args[0]' "$REPO_DIR/.mcp.json"
  assert_output "project"
}

@test "перекрытие посерверное: слой меняет env, command/args остаются нижними" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"srv": {"command": "echo", "args": ["keep"], "env": {"A": "1"}}}}
EOF
  echo '{"mcpServers":{"srv":{"env":{"A":"2"}}}}' > "$REPO_DIR/.agents/mcp.local.json"

  wire
  run jq -r '.mcpServers.srv | "\(.command) \(.args[0]) \(.env.A)"' "$REPO_DIR/.mcp.json"
  assert_output "echo keep 2"
}

@test "null в локальном слое выключает платформенный сервер" {
  # Два сервера намеренно: если выключить единственный, движок сносит .mcp.json
  # целиком (COUNT=0), и тест проверял бы не выключение, а удаление файла.
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"srv": {"command": "echo", "args": ["global"]},
                "keep": {"command": "echo", "args": ["stay"]}}}
EOF
  echo '{"mcpServers":{"srv":null}}' > "$REPO_DIR/.agents/mcp.local.json"

  wire
  assert_success
  run jq -r '.mcpServers | has("srv")' "$REPO_DIR/.mcp.json"
  assert_output "false"
  run jq -r '.mcpServers | has("keep")' "$REPO_DIR/.mcp.json"
  assert_output "true"
}

@test "секрет из пользовательского стора виден проекту (токен вписан один раз на машину)" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"srv": {"command": "echo", "args": ["${SHARED_TOKEN}"]}}}
EOF
  printf 'SHARED_TOKEN=from-store\n' > "$USER_STORE/secrets.env"

  wire
  run jq -r '.mcpServers.srv.args[0]' "$REPO_DIR/.mcp.json"
  assert_output "from-store"
}

@test "проектный секрет перекрывает пользовательский" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"srv": {"command": "echo", "args": ["${SHARED_TOKEN}"]}}}
EOF
  printf 'SHARED_TOKEN=from-store\n'   > "$USER_STORE/secrets.env"
  printf 'SHARED_TOKEN=from-project\n' > "$REPO_DIR/.agents/mcp.secrets.env"

  wire
  run jq -r '.mcpServers.srv.args[0]' "$REPO_DIR/.mcp.json"
  assert_output "from-project"
}

@test "--dump-plan: валидный JSON с провенансом по слоям" {
  seed_global
  echo '{"mcpServers":{"mine":{"command":"echo","args":["u"]}}}' > "$USER_STORE/servers.json"
  echo '{"mcpServers":{"srv":{"args":["overridden"]}}}'          > "$REPO_DIR/.agents/mcp.local.json"

  wire --dump-plan
  assert_success
  # План сохраняем: каждый следующий `run` затирает $output, и проверки со
  # второй начали бы разбирать вывод предыдущего jq вместо плана.
  local plan="$output"

  run jq -r '.origins.srv.scope'      <<<"$plan"; assert_output "local"
  run jq -r '.origins.srv.shadows[0]' <<<"$plan"; assert_output "global"
  run jq -r '.origins.mine.scope'     <<<"$plan"; assert_output "user"
  run jq -r '.active | sort | join(",")' <<<"$plan"; assert_output "mine,srv"
}

@test "--dump-plan: отсечённый сервер попадает в reasons с именем переменной" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"gated": {"x-requires": ["env:NEEDED_VAR"], "command": "echo"}}}
EOF
  wire --dump-plan
  assert_success
  local plan="$output"
  run jq -r '.reasons.gated.need_env' <<<"$plan"; assert_output "NEEDED_VAR"
  run jq -r '.active | length'        <<<"$plan"; assert_output "0"
}

@test "--dump-plan: секретов в выводе нет, подстановка остаётся неразвёрнутой" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"srv": {"command": "echo", "args": ["${SHARED_TOKEN}"]}}}
EOF
  printf 'SHARED_TOKEN=super-secret-value\n' > "$USER_STORE/secrets.env"

  wire --dump-plan
  assert_success
  refute_output --partial "super-secret-value"
  local plan="$output"
  run jq -r '.defs.srv.args[0]' <<<"$plan"
  assert_output '${SHARED_TOKEN}'
}

@test "--dump-plan ничего не записывает: .mcp.json не появляется" {
  seed_global
  wire --dump-plan
  assert_success
  [ ! -e "$REPO_DIR/.mcp.json" ]
  [ ! -e "$REPO_DIR/.agents/mcp.secrets.env" ]
}

@test "битый JSON в локальном слое: раздача отказывается, а не молчит" {
  seed_global
  echo '{"mcpServers": {' > "$REPO_DIR/.agents/mcp.local.json"

  wire
  assert_failure
  assert_output --partial "невалидный JSON"
}

@test ".gitignore получает локальный слой даже если блок MCP дописан раньше" {
  seed_global
  # Проект «из прошлой версии платформы»: .mcp.json и секреты уже перечислены,
  # а локального слоя ещё нет — именно так выглядят все заведённые ранее репо.
  printf '/.mcp.json\n/.agents/mcp.secrets.env\n' > "$REPO_DIR/.gitignore"

  wire
  assert_success
  run grep -c "mcp.local.json" "$REPO_DIR/.gitignore"
  assert_output "1"
}

@test "определение платформенного репозитория не зависит от readlink -f" {
  # На macOS-хосте у readlink нет -f: обе подстановки давали пустую строку,
  # сравнение "" = "" совпадало, и платформой объявлялся ЛЮБОЙ проект — то есть
  # adc mcp list в проекте молча отвечал «MCP тут не раздаются».
  seed_global
  fake_bin="$(mktemp -d)"
  printf '#!/bin/sh\nexit 1\n' > "$fake_bin/readlink"
  chmod +x "$fake_bin/readlink"

  PATH="$fake_bin:$PATH" wire --dump-plan
  assert_success
  run jq -r '.platform_repo // "no"' <<<"$output"
  assert_output "no"
  rm -rf "$fake_bin"
}

@test "migrate-state: mcp-state переезжает из .claude и раздача продолжает обновляться" {
  # Ключевой случай: state-файл на старом месте + уже лежащий .mcp.json. Без
  # переезда сработала бы ветка «раскладывали его не мы», и .mcp.json навсегда
  # замёрз бы на старом наборе серверов — молча.
  seed_global
  mkdir -p "$REPO_DIR/.claude"
  echo "oldserver" > "$REPO_DIR/.claude/.ai-devcontainer-mcp"
  echo '{"mcpServers":{"oldserver":{"command":"echo"}}}' > "$REPO_DIR/.mcp.json"

  wire
  assert_success
  [ -f "$REPO_DIR/.ai-devcontainer/mcp-state" ]
  [ ! -e "$REPO_DIR/.claude/.ai-devcontainer-mcp" ]

  run jq -r '.mcpServers | has("srv")' "$REPO_DIR/.mcp.json"
  assert_output "true"
  run jq -r '.mcpServers | has("oldserver")' "$REPO_DIR/.mcp.json"
  assert_output "false"
}

@test "чужой .mcp.json без state-файла по-прежнему не трогаем" {
  seed_global
  echo '{"mcpServers":{"handmade":{"command":"echo"}}}' > "$REPO_DIR/.mcp.json"

  wire
  assert_success
  run jq -r '.mcpServers | has("handmade")' "$REPO_DIR/.mcp.json"
  assert_output "true"
}
