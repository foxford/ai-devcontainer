#!/usr/bin/env bats
# bin/ai-devcontainer — check_project_devcontainer / cmd_doctor / диспетчер.
# check_project_devcontainer вызывается через `doctor` (единственный публичный
# путь до него без служебных флагов).

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/fixtures'

  BIN="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)/bin/ai-devcontainer"
  PLATFORM_FIXTURE="$(make_platform_fixture)"
  REPO_DIR="$(make_repo_fixture)"
}

teardown() {
  cleanup_fixture "$PLATFORM_FIXTURE"
  cleanup_fixture "$REPO_DIR"
}

run_doctor() {
  AI_DEVCONTAINER_HOME="$PLATFORM_FIXTURE" REPO_ROOT="$REPO_DIR" run bash "$BIN" doctor
}

full_devcontainer_json() {
  cat > "$REPO_DIR/.devcontainer/devcontainer.json" <<'EOF'
{
  "initializeCommand": "ai-devcontainer prepare || \"$HOME/.local/bin/ai-devcontainer\" prepare",
  "mounts": [
    "source=${localEnv:HOME}/.local/share/ai-devcontainer,target=/opt/ai-devcontainer,type=bind,readonly",
    "source=platform-playwright-browsers,target=/home/node/.cache/ms-playwright,type=volume",
    "source=platform-claude-versions,target=/home/node/.local/share/claude,type=volume",
    "source=${localEnv:HOME}/.ai-devcontainer-dev/x/dsh,target=/home/node/.dsh,type=bind"
  ],
  "postCreateCommand": "bash /opt/ai-devcontainer/tooling/post-create-setup.sh"
}
EOF
}

@test "нет devcontainer.json — сообщение без падения" {
  cd "$REPO_DIR" && run bash "$BIN" doctor
  assert_success
  assert_output --partial "devcontainer.json не найден"
}

@test "полный devcontainer.json — всё на месте" {
  mkdir -p "$REPO_DIR/.devcontainer"
  full_devcontainer_json
  run_doctor
  assert_success
  assert_output --partial "devcontainer: всё на месте"
}

@test "нет маунта клона платформы — подсказка с готовой строкой" {
  mkdir -p "$REPO_DIR/.devcontainer"
  echo '{"initializeCommand": "ai-devcontainer prepare"}' > "$REPO_DIR/.devcontainer/devcontainer.json"
  run_doctor
  assert_success
  assert_output --partial "нет маунта клона платформы"
  assert_output --partial "target=/opt/ai-devcontainer"
}

@test "initializeCommand старой формы (ensure-image без prepare) — предупреждение" {
  mkdir -p "$REPO_DIR/.devcontainer"
  full_devcontainer_json
  sed -i 's/ai-devcontainer prepare.*prepare"/ensure-image manual"/' "$REPO_DIR/.devcontainer/devcontainer.json"
  run_doctor
  assert_success
  assert_output --partial "старой формы"
}

@test "нет volume под Playwright — подсказка" {
  mkdir -p "$REPO_DIR/.devcontainer"
  full_devcontainer_json
  sed -i '/ms-playwright/d' "$REPO_DIR/.devcontainer/devcontainer.json"
  run_doctor
  assert_success
  assert_output --partial "браузеры Playwright"
}

@test "docker-compose в репо без docker-in-docker feature — предупреждение" {
  mkdir -p "$REPO_DIR/.devcontainer"
  full_devcontainer_json
  echo "services: {}" > "$REPO_DIR/docker-compose.yml"
  run_doctor
  assert_success
  assert_output --partial "docker-in-docker"
}

@test "закомментированная строка не считается настоящим маунтом" {
  mkdir -p "$REPO_DIR/.devcontainer"
  cat > "$REPO_DIR/.devcontainer/devcontainer.json" <<'EOF'
{
  "initializeCommand": "ai-devcontainer prepare",
  // "source=x,target=/opt/ai-devcontainer,type=bind,readonly"
  "postCreateCommand": "bash /opt/ai-devcontainer/tooling/post-create-setup.sh"
}
EOF
  run_doctor
  assert_success
  assert_output --partial "нет маунта клона платформы"
}

@test "doctor: платформенные факты (скиллы, MCP, скаффолды)" {
  run_doctor
  assert_success
  assert_output --partial "Платформа:"
  assert_output --partial "скаффолды:"
  assert_output --partial "pnpm-monorepo"
}

@test "неизвестная команда верхнего уровня — ошибка" {
  AI_DEVCONTAINER_HOME="$PLATFORM_FIXTURE" run bash "$BIN" bogus-command
  assert_failure
  assert_output --partial "Неизвестная команда"
}

@test "help выводит справку" {
  AI_DEVCONTAINER_HOME="$PLATFORM_FIXTURE" run bash "$BIN" help
  assert_success
  assert_output --partial "ai-devcontainer new"
}
