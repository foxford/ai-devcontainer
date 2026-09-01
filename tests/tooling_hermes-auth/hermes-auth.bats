#!/usr/bin/env bats
# tooling/hermes-auth.sh — общий на все проекты логин Hermes.
#
# И $HERMES_HOME, и стор подменяются переменными окружения, поэтому тесты
# никогда не касаются ни настоящего ~/.hermes, ни /opt/ai-tools.

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/mocks'

  PLATFORM_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  SCRIPT="$PLATFORM_ROOT/tooling/hermes-auth.sh"

  export HERMES_HOME="$(mktemp -d)/hermes"
  export AI_DEVCONTAINER_HERMES_STORE="$(mktemp -d)/store"
  mkdir -p "$HERMES_HOME"

  mocks_init
}

teardown() {
  rm -rf "$(dirname "$HERMES_HOME")" "$(dirname "$AI_DEVCONTAINER_HERMES_STORE")"
  mocks_cleanup
}

run_ha() { run bash "$SCRIPT" "$@"; }

# Правдоподобный auth.json: has_creds смотрит на active_provider/providers.
seed_creds() {
  printf '{"version":1,"active_provider":"%s","providers":{"%s":{"logged_in":true}}}\n' \
    "${1:-openrouter}" "${1:-openrouter}" > "$2"
}

# ── link ──────────────────────────────────────────────────────

@test "link: пустой проект — auth.json и auth.lock становятся симлинками в стор" {
  run_ha link
  assert_success
  [ -L "$HERMES_HOME/auth.json" ]
  [ -L "$HERMES_HOME/auth.lock" ]
  [ "$(readlink -f "$HERMES_HOME/auth.json")" = "$(readlink -f "$AI_DEVCONTAINER_HERMES_STORE/auth.json")" ]
}

@test "link: логин первого проекта уезжает в стор и раздаётся дальше" {
  seed_creds openrouter "$HERMES_HOME/auth.json"
  run_ha link
  assert_success
  assert_output --partial "уехал в общий стор"
  [ -L "$HERMES_HOME/auth.json" ]
  run cat "$AI_DEVCONTAINER_HERMES_STORE/auth.json"
  assert_output --partial "openrouter"
}

@test "link: логин и в проекте, и в сторе — не перетираем ни тот, ни другой" {
  mkdir -p "$AI_DEVCONTAINER_HERMES_STORE"
  seed_creds nous "$AI_DEVCONTAINER_HERMES_STORE/auth.json"
  seed_creds openrouter "$HERMES_HOME/auth.json"
  run_ha link
  assert_success
  assert_output --partial "оставляю проектный"
  [ ! -L "$HERMES_HOME/auth.json" ]
  run cat "$HERMES_HOME/auth.json";                       assert_output --partial "openrouter"
  run cat "$AI_DEVCONTAINER_HERMES_STORE/auth.json";      assert_output --partial "nous"
}

@test "link: чужой симлинк (дотфайлы человека) не трогаем" {
  local elsewhere="$(mktemp -d)/dotfiles-auth.json"
  seed_creds nous "$elsewhere"
  ln -s "$elsewhere" "$HERMES_HOME/auth.json"
  run_ha link
  assert_success
  [ "$(readlink "$HERMES_HOME/auth.json")" = "$elsewhere" ]
}

@test "link: идемпотентен — второй прогон ничего не ломает" {
  seed_creds openrouter "$HERMES_HOME/auth.json"
  run_ha link
  assert_success
  run_ha link
  assert_success
  [ -L "$HERMES_HOME/auth.json" ]
  run cat "$AI_DEVCONTAINER_HERMES_STORE/auth.json"
  assert_output --partial "openrouter"
}

@test "link: стор недоступен — предупреждение, но не падение" {
  export AI_DEVCONTAINER_HERMES_STORE=/proc/nonexistent-store
  run_ha link
  assert_success
  assert_output --partial "нет доступа к стору"
}

# Ключевое свойство раскладки: refresh токенов пишет через atomic_replace,
# который резолвит симлинк (utils.py:61-82). Здесь проверяем нашу половину
# контракта — что запись в реальный файл видна проекту через ссылку.
@test "link: запись в стор видна проекту через симлинк" {
  run_ha link
  assert_success
  seed_creds nous "$AI_DEVCONTAINER_HERMES_STORE/auth.json"
  run cat "$HERMES_HOME/auth.json"
  assert_output --partial "nous"
}

# ── save / unlink ─────────────────────────────────────────────

@test "save: логин проекта перекрывает общий и проект перелинковывается" {
  mkdir -p "$AI_DEVCONTAINER_HERMES_STORE"
  seed_creds nous "$AI_DEVCONTAINER_HERMES_STORE/auth.json"
  seed_creds openrouter "$HERMES_HOME/auth.json"
  run_ha save
  assert_success
  [ -L "$HERMES_HOME/auth.json" ]
  run cat "$AI_DEVCONTAINER_HERMES_STORE/auth.json"
  assert_output --partial "openrouter"
}

@test "save: логина в проекте нет — отказ с подсказкой" {
  run_ha save
  assert_failure
  assert_output --partial "hermes setup"
}

@test "unlink: проект получает свою копию общего логина" {
  seed_creds openrouter "$HERMES_HOME/auth.json"
  run_ha link
  assert_success
  run_ha unlink
  assert_success
  [ ! -L "$HERMES_HOME/auth.json" ]
  run cat "$HERMES_HOME/auth.json"
  assert_output --partial "openrouter"
}

# ── configured ────────────────────────────────────────────────

@test "configured: пустой home — код возврата не 0" {
  run_ha configured
  assert_failure
}

@test "configured: auth.json с активным провайдером — 0" {
  seed_creds openrouter "$HERMES_HOME/auth.json"
  run_ha configured
  assert_success
}

@test "configured: ключ в окружении процесса — 0 даже без auth.json" {
  OPENROUTER_API_KEY=sk-test run bash "$SCRIPT" configured
  assert_success
}

@test "configured: ключ в \$HERMES_HOME/.env — 0" {
  printf 'ANTHROPIC_API_KEY=sk-ant-test\n' > "$HERMES_HOME/.env"
  run_ha configured
  assert_success
}

@test "configured: закомментированный ключ в .env не считается" {
  printf '# OPENROUTER_API_KEY=sk-test\nOPENROUTER_API_KEY=\n' > "$HERMES_HOME/.env"
  run_ha configured
  assert_failure
}

# ── status ────────────────────────────────────────────────────

@test "status: показывает стор, отсутствие логина и команду визарда" {
  run_ha status
  assert_success
  assert_output --partial "$AI_DEVCONTAINER_HERMES_STORE"
  assert_output --partial "общий логин: нет"
  assert_output --partial "hermes setup"
}

@test "status: связанный проект с логином — виден активный провайдер" {
  seed_creds openrouter "$HERMES_HOME/auth.json"
  run bash "$SCRIPT" link
  run_ha status
  assert_success
  assert_output --partial "общий логин: есть"
  assert_output --partial "openrouter"
  assert_output --partial "→ стор (симлинк)"
}
