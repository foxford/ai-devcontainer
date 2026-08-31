#!/usr/bin/env bats
# tooling/wire-docs.sh — чистая файловая логика (симлинки), без внешних
# side-effects. Источник доков переопределяем через AI_DEVCONTAINER_DOCS.

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/fixtures'

  PLATFORM_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  WIRE_DOCS="$PLATFORM_ROOT/tooling/wire-docs.sh"
  REPO_DIR="$(make_repo_fixture)"
  DOCS_SRC="$(mktemp -d)"
}

teardown() {
  cleanup_fixture "$REPO_DIR"
  rm -rf "$DOCS_SRC"
}

run_wire_docs() {
  REPO_ROOT="$REPO_DIR" AI_DEVCONTAINER_DOCS="$DOCS_SRC" run bash "$WIRE_DOCS"
}

@test "нет каталога доков — сообщение, exit 0" {
  rm -rf "$DOCS_SRC"
  run_wire_docs
  assert_success
  assert_output --partial "доки не раздаю"
}

@test "REPO_ROOT совпадает с PLATFORM_ROOT — не раздаём самому себе" {
  REPO_ROOT="$PLATFORM_ROOT" AI_DEVCONTAINER_DOCS="$DOCS_SRC" run bash "$WIRE_DOCS"
  assert_success
  assert_output --partial "репозиторий платформы"
}

@test "AGENTS.platform.md раскладывается симлинком" {
  echo "platform contract" > "$DOCS_SRC/AGENTS.platform.md"
  run_wire_docs
  assert_success
  assert_output --partial "1 разложено"

  [ -L "$REPO_DIR/AGENTS.platform.md" ]
  run cat "$REPO_DIR/AGENTS.platform.md"
  assert_output "platform contract"
}

@test "вложенный путь назначения создаётся (plans/README.md)" {
  echo "plans readme" > "$DOCS_SRC/plans-README.md"
  run_wire_docs
  assert_success
  [ -L "$REPO_DIR/plans/README.md" ]
}

@test "существующий НАСТОЯЩИЙ файл проекта не трогаем" {
  echo "platform contract" > "$DOCS_SRC/AGENTS.platform.md"
  echo "project override" > "$REPO_DIR/AGENTS.platform.md"
  run_wire_docs
  assert_success
  assert_output --partial "1 оставлено за проектом"

  [ ! -L "$REPO_DIR/AGENTS.platform.md" ]
  run cat "$REPO_DIR/AGENTS.platform.md"
  assert_output "project override"
}

@test "отсутствующий source-файл — предупреждение, но не падение" {
  # DOCS_SRC пуст: ни один из 5 файлов не существует.
  run_wire_docs
  assert_success
  assert_output --partial "нет"
  assert_output --partial "0 разложено"
}

@test "повторный прогон идемпотентен" {
  echo "platform contract" > "$DOCS_SRC/AGENTS.platform.md"
  run_wire_docs
  assert_success
  run_wire_docs
  assert_success
  assert_output --partial "1 разложено"
  [ -L "$REPO_DIR/AGENTS.platform.md" ]
}
