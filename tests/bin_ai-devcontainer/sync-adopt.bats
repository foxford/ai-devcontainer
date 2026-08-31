#!/usr/bin/env bats
# bin/ai-devcontainer — cmd_sync / cmd_adopt_devcontainer / is_unmanaged_repo /
# guess_scaffold_type. sub-скрипты (skill.sh, wire-mcp.sh) — заглушки, они
# протестированы отдельно.

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/fixtures'

  BIN="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)/bin/ai-devcontainer"
  PLATFORM_FIXTURE="$(make_platform_fixture)"
  printf '#!/usr/bin/env bash\ntrue\n' > "$PLATFORM_FIXTURE/tooling/skill.sh"
  printf '#!/usr/bin/env bash\ntrue\n' > "$PLATFORM_FIXTURE/tooling/wire-mcp.sh"
  chmod +x "$PLATFORM_FIXTURE/tooling/skill.sh" "$PLATFORM_FIXTURE/tooling/wire-mcp.sh"

  REPO_DIR="$(make_repo_fixture)"
}

teardown() {
  cleanup_fixture "$PLATFORM_FIXTURE"
  cleanup_fixture "$REPO_DIR"
}

run_sync() {
  AI_DEVCONTAINER_HOME="$PLATFORM_FIXTURE" REPO_ROOT="$REPO_DIR" run bash "$BIN" sync "$@"
}

@test "sync: репозиторий без .devcontainer/devcontainer.json — предупреждает, но раскладывает" {
  run_sync
  assert_success
  assert_output --partial "не заведён под платформу"
  assert_output --partial "sync --adopt"
  assert_output --partial "Применяю платформу"
}

@test "sync: devcontainer.json есть, но без вызова prepare — тоже unmanaged" {
  mkdir -p "$REPO_DIR/.devcontainer"
  echo '{"name": "old-style"}' > "$REPO_DIR/.devcontainer/devcontainer.json"
  run_sync
  assert_success
  assert_output --partial "не заведён под платформу"
}

@test "sync: devcontainer.json с вызовом prepare — managed, без предупреждения" {
  mkdir -p "$REPO_DIR/.devcontainer"
  echo '{"initializeCommand": "ai-devcontainer prepare"}' > "$REPO_DIR/.devcontainer/devcontainer.json"
  run_sync
  assert_success
  refute_output --partial "не заведён под платформу"
}

@test "sync --adopt: заводит .devcontainer/ из скаффолда, код не трогает" {
  echo "existing project code" > "$REPO_DIR/app.js"
  run_sync --adopt
  assert_success
  assert_output --partial "adopt: добавляю .devcontainer/"

  [ -f "$REPO_DIR/.devcontainer/devcontainer.json" ]
  [ -f "$REPO_DIR/.devcontainer/Dockerfile" ]
  run cat "$REPO_DIR/app.js"
  assert_output "existing project code"
}

@test "sync --adopt: {{NAME}} подставлен по basename репозитория, файлы вне .devcontainer/ не переименовываются" {
  run_sync --adopt
  assert_success
  run cat "$REPO_DIR/.devcontainer/devcontainer.json"
  assert_output --partial "$(basename "$REPO_DIR")"
}

@test "sync --adopt: уже есть непустой .devcontainer/ — отказ" {
  mkdir -p "$REPO_DIR/.devcontainer"
  echo '{"name": "manual"}' > "$REPO_DIR/.devcontainer/devcontainer.json"
  run_sync --adopt
  assert_failure
  assert_output --partial "уже существует и не пуст"
}

@test "sync --adopt: угадывает pnpm-monorepo по package.json+pnpm-lock.yaml" {
  echo '{}' > "$REPO_DIR/package.json"
  echo "lockfileVersion: 6" > "$REPO_DIR/pnpm-lock.yaml"
  run_sync --adopt
  assert_success
  assert_output --partial "pnpm-monorepo"
}

@test "sync: неизвестный флаг — ошибка" {
  run_sync --bogus
  assert_failure
  assert_output --partial "неизвестный аргумент"
}
