#!/usr/bin/env bats
# bin/adc — check_project_devcontainer / cmd_doctor / диспетчер.
# check_project_devcontainer вызывается через `doctor` (единственный публичный
# путь до него без служебных флагов).

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/fixtures'

  BIN="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)/bin/adc"
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
  "initializeCommand": "adc prepare || \"$HOME/.local/bin/adc\" prepare",
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
  echo '{"initializeCommand": "adc prepare"}' > "$REPO_DIR/.devcontainer/devcontainer.json"
  run_doctor
  assert_success
  assert_output --partial "нет маунта клона платформы"
  assert_output --partial "target=/opt/ai-devcontainer"
}

@test "initializeCommand старой формы (ensure-image без prepare) — предупреждение" {
  mkdir -p "$REPO_DIR/.devcontainer"
  full_devcontainer_json
  sed -i 's/adc prepare.*prepare"/ensure-image manual"/' "$REPO_DIR/.devcontainer/devcontainer.json"
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

# COPY-манифесты: усыновлённый репозиторий без .tool-versions собирается до
# «failed to compute cache key», и по этой ошибке про devcontainer не догадаться.
copy_dockerfile() {
  cat > "$REPO_DIR/.devcontainer/devcontainer.json" <<'EOF'
{
  "initializeCommand": "adc prepare",
  "build": { "dockerfile": "Dockerfile", "context": ".." }
}
EOF
  printf 'FROM dev-base:local\nCOPY --chown=node:node package.json .tool-versions /tmp/repo/\n' \
    > "$REPO_DIR/.devcontainer/Dockerfile"
}

@test "Dockerfile COPY'ит файл, которого нет в корне — предупреждение с диагнозом" {
  mkdir -p "$REPO_DIR/.devcontainer"
  copy_dockerfile
  echo '{}' > "$REPO_DIR/package.json"
  run_doctor
  assert_success
  assert_output --partial ".tool-versions"
  assert_output --partial "failed to compute cache key"
  refute_output --partial "COPY'ит «package.json»"
}

@test "все COPY-манифесты на месте — про них молчим" {
  mkdir -p "$REPO_DIR/.devcontainer"
  copy_dockerfile
  echo '{}' > "$REPO_DIR/package.json"
  echo "nodejs 26.5.0" > "$REPO_DIR/.tool-versions"
  run_doctor
  assert_success
  refute_output --partial "failed to compute cache key"
}

@test "COPY --from=<стадия> и шаблоны не проверяются: они не из контекста" {
  mkdir -p "$REPO_DIR/.devcontainer"
  copy_dockerfile
  echo '{}' > "$REPO_DIR/package.json"
  echo "nodejs 26.5.0" > "$REPO_DIR/.tool-versions"
  printf 'COPY --from=builder /app/dist /srv\nCOPY pnpm-lock*.yaml /tmp/\n' \
    >> "$REPO_DIR/.devcontainer/Dockerfile"
  run_doctor
  assert_success
  refute_output --partial "failed to compute cache key"
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
  "initializeCommand": "adc prepare",
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
  assert_output --partial "adc new"
}
