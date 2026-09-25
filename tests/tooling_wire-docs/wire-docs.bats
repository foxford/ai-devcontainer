#!/usr/bin/env bats
# tooling/wire-docs.sh — чистая файловая логика (симлинк/копия), без внешних
# side-effects. Источник доков переопределяем через AI_DEVCONTAINER_DOCS.
#
# Главное, что здесь закреплено: AGENTS.platform.md раздаётся КОПИЕЙ, а не
# симлинком. Его тянет `@`-импортом CLAUDE.md, а импорт в симлинк Claude Code
# не разворачивает — контракт платформы молча не доезжает до контекста, причём
# раздача при этом рапортует «разложено» и файл на месте.

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

# Тихий прогон: когда проверяем результат на диске, а не вывод.
wire_docs_quiet() {
  REPO_ROOT="$REPO_DIR" AI_DEVCONTAINER_DOCS="$DOCS_SRC" bash "$WIRE_DOCS" >/dev/null 2>&1
}

# ── гарды ─────────────────────────────────────────────────────

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

# ── режим copy: AGENTS.platform.md ────────────────────────────

@test "AGENTS.platform.md раскладывается КОПИЕЙ — @-импорт в симлинк не работает" {
  echo "platform contract" > "$DOCS_SRC/AGENTS.platform.md"
  run_wire_docs
  assert_success
  assert_output --partial "1 разложено"

  [ ! -L "$REPO_DIR/AGENTS.platform.md" ]
  [ -f "$REPO_DIR/AGENTS.platform.md" ]
  run cat "$REPO_DIR/AGENTS.platform.md"
  assert_output --partial "platform contract"
}

@test "копия несёт маркер в ПЕРВОЙ строке — по ней раздача узнаёт своё" {
  echo "platform contract" > "$DOCS_SRC/AGENTS.platform.md"
  wire_docs_quiet
  run head -1 "$REPO_DIR/AGENTS.platform.md"
  assert_output --partial "ai-devcontainer:generated"
}

@test "копия обновляется, когда платформа уехала" {
  echo "contract v1" > "$DOCS_SRC/AGENTS.platform.md"
  wire_docs_quiet
  echo "contract v2" > "$DOCS_SRC/AGENTS.platform.md"
  wire_docs_quiet

  run cat "$REPO_DIR/AGENTS.platform.md"
  assert_output --partial "contract v2"
  refute_output --partial "contract v1"
}

@test "совпадающую копию не переписываем — postCreate идёт на каждый старт" {
  # Проверяем по inode: mv дал бы новый. Дёргать mtime дока, который никто не
  # менял, незачем — это замечают редакторы и watch-режимы.
  echo "platform contract" > "$DOCS_SRC/AGENTS.platform.md"
  wire_docs_quiet
  local before; before="$(ls -i "$REPO_DIR/AGENTS.platform.md" | awk '{print $1}')"
  wire_docs_quiet
  local after;  after="$(ls -i "$REPO_DIR/AGENTS.platform.md" | awk '{print $1}')"
  [ "$before" = "$after" ]
}

@test "миграция со старой схемы: симлинк прошлой раздачи становится копией" {
  # Живые проекты пришли из схемы, где док раздавался симлинком. Пересобранный
  # контейнер обязан их вылечить сам — иначе Claude так и не увидит контракт.
  echo "platform contract" > "$DOCS_SRC/AGENTS.platform.md"
  ln -sfn "$DOCS_SRC/AGENTS.platform.md" "$REPO_DIR/AGENTS.platform.md"
  run_wire_docs
  assert_success

  [ ! -L "$REPO_DIR/AGENTS.platform.md" ]
  run head -1 "$REPO_DIR/AGENTS.platform.md"
  assert_output --partial "ai-devcontainer:generated"
}

# ── режим link: всё остальное ─────────────────────────────────

@test "MONOREPO.md остаётся симлинком — его @-импортом никто не тянет" {
  echo "monorepo doc" > "$DOCS_SRC/MONOREPO.md"
  run_wire_docs
  assert_success
  [ -L "$REPO_DIR/MONOREPO.md" ]
  run cat "$REPO_DIR/MONOREPO.md"
  assert_output "monorepo doc"
}

@test "вложенный путь назначения создаётся (plans/README.md)" {
  echo "plans readme" > "$DOCS_SRC/plans-README.md"
  run_wire_docs
  assert_success
  [ -L "$REPO_DIR/plans/README.md" ]
}

@test "битый симлинк прошлой раздачи чинится, а не бережётся" {
  echo "monorepo doc" > "$DOCS_SRC/MONOREPO.md"
  ln -sfn /nope/gone "$REPO_DIR/MONOREPO.md"
  run_wire_docs
  assert_success
  run cat "$REPO_DIR/MONOREPO.md"
  assert_output "monorepo doc"
}

# ── перекрытие проектом ───────────────────────────────────────

@test "существующий НАСТОЯЩИЙ файл проекта не трогаем" {
  echo "platform contract" > "$DOCS_SRC/AGENTS.platform.md"
  echo "project override" > "$REPO_DIR/AGENTS.platform.md"
  run_wire_docs
  assert_success
  assert_output --partial "1 оставлено за проектом"

  run cat "$REPO_DIR/AGENTS.platform.md"
  assert_output "project override"
}

@test "перекрытие держится и на повторных прогонах" {
  echo "platform contract" > "$DOCS_SRC/AGENTS.platform.md"
  echo "project override" > "$REPO_DIR/AGENTS.platform.md"
  wire_docs_quiet
  wire_docs_quiet
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
  echo "monorepo doc"      > "$DOCS_SRC/MONOREPO.md"
  run_wire_docs
  assert_success
  run_wire_docs
  assert_success
  assert_output --partial "2 разложено"
  [ -f "$REPO_DIR/AGENTS.platform.md" ] && [ ! -L "$REPO_DIR/AGENTS.platform.md" ]
  [ -L "$REPO_DIR/MONOREPO.md" ]
}

# ── флаги для соседних скриптов ───────────────────────────────
# Их зовёт tooling/skill.sh, чтобы не держать вторую копию списка доков и
# второй разбор «наше ли это». Отвечать обязаны без проекта и без доков.

@test "--list печатает таблицу раздачи с режимами" {
  run bash "$WIRE_DOCS" --list
  assert_success
  assert_line "AGENTS.platform.md:AGENTS.platform.md:copy"
  assert_line "MONOREPO.md:MONOREPO.md:link"
  refute_output --partial "разложено"
}

@test "--list работает и в самом репозитории платформы (гард его не глотает)" {
  REPO_ROOT="$PLATFORM_ROOT" run bash "$WIRE_DOCS" --list
  assert_success
  assert_line "AGENTS.platform.md:AGENTS.platform.md:copy"
}

@test "--is-generated: копия режима copy — наша" {
  echo "platform contract" > "$DOCS_SRC/AGENTS.platform.md"
  wire_docs_quiet
  run bash "$WIRE_DOCS" --is-generated "$REPO_DIR/AGENTS.platform.md"
  assert_success
}

@test "--is-generated: симлинк режима link — наш" {
  echo "monorepo doc" > "$DOCS_SRC/MONOREPO.md"
  wire_docs_quiet
  run bash "$WIRE_DOCS" --is-generated "$REPO_DIR/MONOREPO.md"
  assert_success
}

@test "--is-generated: файл проекта — не наш" {
  echo "project override" > "$REPO_DIR/AGENTS.platform.md"
  run bash "$WIRE_DOCS" --is-generated "$REPO_DIR/AGENTS.platform.md"
  assert_failure
}

@test "--is-generated: отсутствующий путь и пустой аргумент — не наши" {
  run bash "$WIRE_DOCS" --is-generated "$REPO_DIR/nope.md"
  assert_failure
  run bash "$WIRE_DOCS" --is-generated
  assert_failure
}

@test "--is-generated: каталог на месте дока — не наш, ничего не ломаем" {
  mkdir -p "$REPO_DIR/AGENTS.platform.md"
  run bash "$WIRE_DOCS" --is-generated "$REPO_DIR/AGENTS.platform.md"
  assert_failure
}
