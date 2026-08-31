#!/usr/bin/env bats
# bin/ai-devcontainer — list_scaffold_types / pick_scaffold_type /
# apply_scaffold_rename / cmd_new.

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/fixtures'

  BIN="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)/bin/ai-devcontainer"
  PLATFORM_FIXTURE="$(make_platform_fixture)"
  DEST_DIR="$(mktemp -d)/newproj"
}

teardown() {
  cleanup_fixture "$PLATFORM_FIXTURE"
  rm -rf "$(dirname "$DEST_DIR")"
}

run_bin() {
  AI_DEVCONTAINER_HOME="$PLATFORM_FIXTURE" run bash "$BIN" "$@"
}

@test "new: единственный тип скаффолда выбирается молча" {
  run_bin new smoke "$DEST_DIR"
  assert_success
  assert_output --partial "тип: pnpm-monorepo"
  [ -f "$DEST_DIR/package.json" ]
}

@test "new: --type явный совпадает с единственным доступным" {
  run_bin new smoke "$DEST_DIR" --type pnpm-monorepo
  assert_success
  [ -f "$DEST_DIR/package.json" ]
}

@test "new: --type неизвестный — ошибка" {
  run_bin new smoke "$DEST_DIR" --type rails
  assert_failure
  assert_output --partial "неизвестный тип скаффолда"
}

@test "new: несколько типов без --type и без TTY — ошибка" {
  mkdir -p "$PLATFORM_FIXTURE/skeleton/go-service/.devcontainer"
  echo '{"label": "Go"}' > "$PLATFORM_FIXTURE/skeleton/go-service/.scaffold.json"
  run_bin new smoke "$DEST_DIR"
  assert_failure
  assert_output --partial "нет TTY для выбора"
}

@test "new: {{NAME}} подставляется через apply_scaffold_rename" {
  run_bin new my-cool-app "$DEST_DIR"
  assert_success
  run cat "$DEST_DIR/package.json"
  assert_output --partial '"my-cool-app"'
  refute_output --partial '"my-project"'
}

@test "new: DEST существует и не пуст — отказ" {
  mkdir -p "$DEST_DIR"
  echo x > "$DEST_DIR/keep.txt"
  run_bin new smoke "$DEST_DIR"
  assert_failure
  assert_output --partial "уже существует и не пуст"
}

@test "new: создаёт git-коммит в новом проекте" {
  run_bin new smoke "$DEST_DIR"
  assert_success
  run git -C "$DEST_DIR" log --oneline
  assert_success
  assert_output --partial "bootstrap from ai-devcontainer skeleton"
}

@test "new: без имени проекта — ошибка использования" {
  run_bin new
  assert_failure
}
