#!/usr/bin/env bats
# tooling/post-create-setup.sh — линейный скрипт без модульных точек входа
# (нет функций верхнего уровня). Тестируем end-to-end: копируем реальный
# скрипт в фикстурное дерево платформы (TOOLING_DIR вычисляется от своего
# расположения, поэтому install-ai-tools.sh/wire-mcp.sh/bin/adc
# рядом с ним обязаны быть фикстурными заглушками, не настоящими).

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/mocks'

  REAL_PLATFORM_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"

  PLATFORM_FIXTURE="$(mktemp -d)"
  mkdir -p "$PLATFORM_FIXTURE/tooling/helpers" "$PLATFORM_FIXTURE/bin" "$PLATFORM_FIXTURE/skeleton/.hermes"
  cp "$REAL_PLATFORM_ROOT/tooling/post-create-setup.sh" "$PLATFORM_FIXTURE/tooling/post-create-setup.sh"

  # Seed-источник .hermes: нужен хотя бы один не-.gitkeep файл, иначе
  # seed_dir считает шаблон пустым и не сидит вовсе.
  printf '#!/usr/bin/env bash\necho bootstrap\n' > "$PLATFORM_FIXTURE/skeleton/.hermes/bootstrap.sh"

  printf '#!/usr/bin/env bash\ntrue\n' > "$PLATFORM_FIXTURE/tooling/install-ai-tools.sh"
  printf '#!/usr/bin/env bash\ntrue\n' > "$PLATFORM_FIXTURE/tooling/wire-mcp.sh"
  printf '#!/usr/bin/env bash\ntrue\n' > "$PLATFORM_FIXTURE/bin/adc"
  chmod +x "$PLATFORM_FIXTURE/tooling/install-ai-tools.sh" "$PLATFORM_FIXTURE/tooling/wire-mcp.sh" "$PLATFORM_FIXTURE/bin/adc"

  PROJECT_DIR="$(mktemp -d)"

  mocks_init
  mock_bin pnpm 0 ""
  export AI_DEVCONTAINER_SKIP_BROWSERS=1   # секция 7 не в фокусе этих тестов
  export HOME_BACKUP="$HOME"
  export HOME="$(mktemp -d)"               # ~/.local/bin изолирован от реального хоста
}

teardown() {
  rm -rf "$PLATFORM_FIXTURE" "$PROJECT_DIR" "$HOME"
  HOME="$HOME_BACKUP"
  mocks_cleanup
}

run_postcreate() {
  PROJECT_ROOT="$PROJECT_DIR" run bash "$PLATFORM_FIXTURE/tooling/post-create-setup.sh"
}

@test "seed: .hermes копируется из шаблона платформы, если в проекте его нет" {
  run_postcreate
  assert_success
  [ -f "$PROJECT_DIR/.hermes/bootstrap.sh" ]
  assert_output --partial "seed: .hermes"
}

@test "seed: существующий .hermes проекта не перезаписывается" {
  mkdir -p "$PROJECT_DIR/.hermes"
  echo "project-owned" > "$PROJECT_DIR/.hermes/marker.txt"
  run_postcreate
  assert_success
  [ -f "$PROJECT_DIR/.hermes/marker.txt" ]
  [ ! -f "$PROJECT_DIR/.hermes/bootstrap.sh" ]
}

@test "pnpm install вызывается" {
  run_postcreate
  assert_success
  run cat "$MOCK_CALLS_DIR/pnpm.log"
  assert_output --partial "install"
}

@test "хелперы копируются в ~/.local/bin, CLI-симлинки создаются" {
  echo "helper body" > "$PLATFORM_FIXTURE/tooling/helpers/demo-helper.sh"
  run_postcreate
  assert_success
  [ -x "$HOME/.local/bin/demo-helper" ]
  [ -L "$HOME/.local/bin/adc" ]
  [ ! -e "$HOME/.local/bin/ai-devcontainer" ]
}

@test "install-ai-tools.sh падает — постCreate прерывается с exit 1" {
  printf '#!/usr/bin/env bash\nexit 1\n' > "$PLATFORM_FIXTURE/tooling/install-ai-tools.sh"
  run_postcreate
  assert_failure
  assert_output --partial "install-ai-tools.sh failed"
}

@test "graphify не в PATH — предупреждение, не падает" {
  run_postcreate
  assert_success
  assert_output --partial "graphify не найден в PATH"
}

@test "graphify есть, граф уже собран — пропускает повторную сборку" {
  mkdir -p "$PROJECT_DIR/graphify-out"
  echo "<html></html>" > "$PROJECT_DIR/graphify-out/graph.html"
  cat > "$MOCK_BIN_DIR/graphify" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/graphify"
  run_postcreate
  assert_success
  assert_output --partial "graph.html уже есть, пропускаю"
}

@test "adc sync вызывается" {
  cat > "$PLATFORM_FIXTURE/bin/adc" <<EOF
#!/usr/bin/env bash
echo "\$@" >> "$MOCK_CALLS_DIR/adc.log"
exit 0
EOF
  chmod +x "$PLATFORM_FIXTURE/bin/adc"
  run_postcreate
  assert_success
  run cat "$MOCK_CALLS_DIR/adc.log"
  assert_output --partial "sync"
}

@test "adc sync падает — предупреждает, но не блокирует" {
  cat > "$PLATFORM_FIXTURE/bin/adc" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
  chmod +x "$PLATFORM_FIXTURE/bin/adc"
  run_postcreate
  assert_success
  assert_output --partial "adc sync failed"
}

@test "нет .hermes/ в проекте после сида (шаблон платформы пуст) — предупреждение на шаге 8" {
  rm -f "$PLATFORM_FIXTURE/skeleton/.hermes/bootstrap.sh"   # шаблон снова пуст → не сидится
  run_postcreate
  assert_success
  assert_output --partial "нет .hermes/ — пропускаю"
}

@test "hermes не в PATH — предупреждение с подсказкой" {
  run_postcreate
  assert_success
  assert_output --partial "hermes CLI не в PATH"
}

@test "AI_DEVCONTAINER_SKIP_BROWSERS пропускает секцию браузеров" {
  run_postcreate
  assert_success
  assert_output --partial "AI_DEVCONTAINER_SKIP_BROWSERS выставлен — пропускаю"
}

@test "полный прогон идемпотентен (повторный запуск не падает)" {
  run_postcreate
  assert_success
  run_postcreate
  assert_success
}
