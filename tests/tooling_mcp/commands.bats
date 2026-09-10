#!/usr/bin/env bats
# tooling/mcp.sh — команды человека поверх движка раздачи.
#
# Проверяем контракт, а не вывод: в какой ФАЙЛ попал сервер, что стало со
# слоями, отказывается ли команда там, где обязана отказаться. Формат
# отрисовки списка намеренно не фиксируем — он будет меняться.

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/fixtures'

  PLATFORM_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  MCP_SH="$PLATFORM_ROOT/tooling/mcp.sh"

  REPO_DIR="$(make_repo_fixture)"
  mkdir -p "$REPO_DIR/.agents"
  touch "$REPO_DIR/.gitignore"
  FIXTURE_SERVERS="$(mktemp)"
  USER_STORE="$(mktemp -d)"
  echo '{"mcpServers": {"plat": {"command": "echo", "args": ["global"]}}}' > "$FIXTURE_SERVERS"

  # Раздача внутри команд не должна трогать настоящие конфиги агентов.
  export CODEX_HOME="$(mktemp -d)"
  export HERMES_HOME="$(mktemp -d)"
  export DSH_HOME="$(mktemp -d)/nonexistent"

  LOCAL_LAYER="$REPO_DIR/.agents/mcp.local.json"
  PROJECT_LAYER="$REPO_DIR/.agents/mcp.json"
  USER_LAYER="$USER_STORE/servers.json"
}

teardown() {
  cleanup_fixture "$REPO_DIR"
  rm -f "$FIXTURE_SERVERS"
  rm -rf "$USER_STORE"
}

mcp() {
  REPO_ROOT="$REPO_DIR" \
  AI_DEVCONTAINER_MCP="$FIXTURE_SERVERS" \
  AI_DEVCONTAINER_MCP_STORE="$USER_STORE" \
  run bash "$MCP_SH" "$@" </dev/null
}

@test "add --json: сниппет из README кладётся как есть, имя берётся из него" {
  mcp add --json '{"mcpServers":{"context7":{"command":"npx","args":["-y","@upstash/context7-mcp"]}}}' --local
  assert_success
  run jq -r '.mcpServers.context7.args[1]' "$LOCAL_LAYER"
  assert_output "@upstash/context7-mcp"
}

@test "add --json с несколькими серверами без имени — отказ, а не молчаливый выбор" {
  mcp add --json '{"mcpServers":{"a":{"command":"x"},"b":{"command":"y"}}}' --local
  assert_failure
  assert_output --partial "назови нужный"
}

@test "add -- команда: аргументы вида -y не ломают разбор" {
  mcp add sentry --local -- npx -y @sentry/mcp-server@0.4.0
  assert_success
  run jq -rc '.mcpServers.sentry.args' "$LOCAL_LAYER"
  assert_output '["-y","@sentry/mcp-server@0.4.0"]'
}

@test "add --url --header: заголовок попадает в определение сервера" {
  mcp add notion --project --url https://mcp.example.com/mcp --header 'Authorization: Bearer ${TOK}'
  assert_success
  run jq -r '.mcpServers.notion.headers.Authorization' "$PROJECT_LAYER"
  assert_output 'Bearer ${TOK}'
  run jq -r '.mcpServers.notion.type' "$PROJECT_LAYER"
  assert_output "http"
}

@test "add --user пишет в общий стор, а не в репозиторий" {
  mcp add mine --user -- node server.js
  assert_success
  run jq -r '.mcpServers.mine.command' "$USER_LAYER"
  assert_output "node"
  [ ! -e "$LOCAL_LAYER" ]
  [ ! -e "$PROJECT_LAYER" ]
}

@test "add --global отклоняется: платформенный слой руками не правят" {
  mcp add nope --global -- node x.js
  assert_failure
  assert_output --partial "глобальный слой"
}

@test "add без слоя и без TTY берёт локальный и говорит об этом" {
  # Тихо положить личный сервер в гит команды — ровно та ошибка, которую потом
  # никто не замечает, поэтому дефолт обязан быть самым узким.
  mcp add quiet -- node x.js
  assert_success
  assert_output --partial "беру локальный"
  run jq -r '.mcpServers.quiet.command' "$LOCAL_LAYER"
  assert_output "node"
}

@test "add отвергает имя, которое молча пропустит DSH" {
  mcp add "bad name" --local -- node x.js
  assert_failure
  assert_output --partial "A-Za-z0-9_-"
}

@test "add без описания сервера печатает три способа, а не общую ошибку" {
  mcp add something --local
  assert_failure
  assert_output --partial "--json"
  assert_output --partial "--url"
}

@test "rm находит слой сам, когда он не указан" {
  mcp add tmp --user -- node x.js
  assert_success
  mcp rm tmp
  assert_success
  assert_output --partial "слоя «user»"
  run jq -r '.mcpServers | has("tmp")' "$USER_LAYER"
  assert_output "false"
}

@test "disable перекрывает платформенный сервер через null" {
  mcp disable plat --local
  assert_success
  run jq -r '.mcpServers.plat' "$LOCAL_LAYER"
  assert_output "null"
}

@test "enable снимает выключатель, поставленный disable" {
  mcp disable plat --local
  assert_success
  mcp enable plat
  assert_success
  run jq -r '.mcpServers | has("plat")' "$LOCAL_LAYER"
  assert_output "false"
}

@test "enable сервера, которому нужен путь, объясняет причину и не врёт про успех" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"gated": {"x-requires": ["path:/nope/missing"], "command": "echo"}}}
EOF
  mcp enable gated
  assert_failure
  assert_output --partial "/nope/missing"
}

@test "enable без TTY не зависает на вводе токена, а говорит, куда его вписать" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"gated": {"x-requires": ["env:SOME_TOKEN"], "command": "echo"}}}
EOF
  mcp enable gated
  assert_success
  assert_output --partial "SOME_TOKEN"
}

@test "list показывает все четыре слоя и не падает на пустых" {
  mcp list
  assert_success
  assert_output --partial "ГЛОБАЛЬНЫЕ"
  assert_output --partial "ПОЛЬЗОВАТЕЛЬСКИЕ"
  assert_output --partial "ПРОЕКТНЫЕ"
  assert_output --partial "ЛОКАЛЬНЫЕ"
}

@test "list не создаёт файлов: посмотреть — не то же, что настроить" {
  mcp list
  assert_success
  [ ! -e "$REPO_DIR/.mcp.json" ]
  [ ! -e "$REPO_DIR/.agents/mcp.secrets.env" ]
  [ ! -e "$LOCAL_LAYER" ]
}

@test "list показывает выключенный сервер, а не прячет его" {
  mcp disable plat --local
  assert_success
  mcp list
  assert_success
  assert_output --partial "plat"
  assert_output --partial "выключен в слое"
}

@test "list называет слой, который перекрывает нижний" {
  mcp add plat --local -- echo mine
  assert_success
  mcp list
  assert_success
  assert_output --partial "ПЕРЕКРЫТИЯ"
  assert_output --partial "перекрывает global"
}

@test "list не печатает значения секретов" {
  cat > "$FIXTURE_SERVERS" <<'EOF'
{"mcpServers": {"srv": {"command": "echo", "args": ["${TOK}"]}}}
EOF
  printf 'TOK=super-secret-value\n' > "$USER_STORE/secrets.env"
  mcp list
  assert_success
  refute_output --partial "super-secret-value"
}

# ── секреты и слой, который лежит в гите ─────────────────────
# Проектный слой — единственный коммитится. Вписанный туда токен уезжает всей
# команде и в историю, откуда его уже не вынуть — только отзывать.

SECRET_SNIPPET='{"type":"http","url":"http://x/mcp","headers":{"Authorization":"Bearer 37159574b6c8cb6047ec454eefe90ab"}}'

@test "add --project: литеральный токен выносится в секреты, в слой идёт \${ИМЯ}" {
  mcp add directus --project --json "$SECRET_SNIPPET"
  assert_success
  run jq -r '.mcpServers.directus.headers.Authorization' "$PROJECT_LAYER"
  assert_output 'Bearer ${DIRECTUS_AUTHORIZATION}'
  run grep -c "^DIRECTUS_AUTHORIZATION=37159574b6c8cb6047ec454eefe90ab$" "$REPO_DIR/.agents/mcp.secrets.env"
  assert_output "1"
}

@test "add --project: сам токен в git-слой не попадает ни в каком виде" {
  mcp add directus --project --json "$SECRET_SNIPPET"
  assert_success
  run cat "$PROJECT_LAYER"
  refute_output --partial "37159574b6c8cb6047ec454eefe90ab"
}

@test "add --local: токен не трогаем — слой и так в .gitignore" {
  mcp add mine --local --json "$SECRET_SNIPPET"
  assert_success
  run jq -r '.mcpServers.mine.headers.Authorization' "$LOCAL_LAYER"
  assert_output "Bearer 37159574b6c8cb6047ec454eefe90ab"
}

@test "sync ругается на секрет, вписанный в .agents/mcp.json руками" {
  printf '%s\n' '{"mcpServers":{"directus":'"$SECRET_SNIPPET"'}}' > "$PROJECT_LAYER"
  mcp sync
  assert_success
  assert_output --partial "ОТКРЫТЫМ ТЕКСТОМ"
  assert_output --partial "mcpServers.directus.headers.Authorization"
  assert_output --partial "отзывать"
}

@test "sync молчит, когда в слое \${ИМЯ}, а не значение" {
  printf '%s\n' '{"mcpServers":{"d":{"type":"http","url":"http://x/mcp","headers":{"Authorization":"Bearer ${TOK}"}}}}' > "$PROJECT_LAYER"
  mcp sync
  assert_success
  refute_output --partial "ОТКРЫТЫМ ТЕКСТОМ"
}

@test "fix-secrets: выносит вписанное руками и сервер продолжает работать" {
  printf '%s\n' '{"mcpServers":{"directus":'"$SECRET_SNIPPET"'}}' > "$PROJECT_LAYER"
  mcp fix-secrets
  assert_success
  run jq -r '.mcpServers.directus.headers.Authorization' "$PROJECT_LAYER"
  assert_output 'Bearer ${DIRECTUS_AUTHORIZATION}'
  # Главное: агент по-прежнему получает рабочее значение
  run jq -r '.mcpServers.directus.headers.Authorization' "$REPO_DIR/.mcp.json"
  assert_output "Bearer 37159574b6c8cb6047ec454eefe90ab"
}

@test "fix-secrets: выносить нечего — говорит об этом, файл не портит" {
  mcp add plain --project -- node x.js
  assert_success
  mcp fix-secrets
  assert_success
  assert_output --partial "нет"
  run jq -r '.mcpServers.plain.command' "$PROJECT_LAYER"
  assert_output "node"
}

@test "env-переменная с токеном в git-слое тоже ловится" {
  printf '%s\n' '{"mcpServers":{"s":{"command":"node","env":{"API_TOKEN":"abcdef0123456789"}}}}' > "$PROJECT_LAYER"
  mcp sync
  assert_success
  assert_output --partial "ОТКРЫТЫМ ТЕКСТОМ"
}
