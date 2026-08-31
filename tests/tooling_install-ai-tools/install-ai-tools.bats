#!/usr/bin/env bats
# tooling/install-ai-tools.sh — только gating-логика («уже установлено и
# работает» vs «нет / сломано — переустановить»). Реальные curl|bash/npm/uv
# установки не гоняем — это сеть, вне бюджета теста; проверяем, что нужная
# ветка ДОСТИГАЕТСЯ (или не достигается), через PATH-моки инструментов.

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/mocks'

  PLATFORM_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  INSTALL_SH="$PLATFORM_ROOT/tooling/install-ai-tools.sh"

  mocks_init
  AI_TOOLS_HOME="$(mktemp -d)"

  # Baseline: все инструменты «уже стоят и работают» — скрипт линейный
  # (set -e), первый же незамоканный шаг оборвёт прогон, не дойдя до
  # остальных. Каждый тест переопределяет ровно тот шаг, который проверяет.
  for tool in claude opencode codex dsh hermes strix; do
    cat > "$MOCK_BIN_DIR/$tool" <<EOF
#!/usr/bin/env bash
[ "\$1" = "--version" ] && { echo "$tool 1.0.0"; exit 0; }
exit 0
EOF
    chmod +x "$MOCK_BIN_DIR/$tool"
  done
  cat > "$MOCK_BIN_DIR/graphify" <<'EOF'
#!/usr/bin/env bash
[ "$1" = "--version" ] && { echo "graphify 0.0.0"; exit 0; }
[ "$1" = "install" ] && exit 0
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/graphify"
  mkdir -p "$AI_TOOLS_HOME/bin"
  printf '#!/usr/bin/env bash\ntrue\n' > "$AI_TOOLS_HOME/bin/uv"
  printf '#!/usr/bin/env bash\ntrue\n' > "$AI_TOOLS_HOME/bin/skaffold"
  chmod +x "$AI_TOOLS_HOME/bin/uv" "$AI_TOOLS_HOME/bin/skaffold"

  # Всё, что скрипт пытается реально установить/скачать, если baseline не
  # покрыл шаг — заглушки-паникёры: лучше явный провал, чем сетевой вызов.
  mock_bin curl 1 ""
  mock_bin npm 1 ""
}

teardown() {
  rm -rf "$AI_TOOLS_HOME"
  mocks_cleanup
}

run_install() {
  AI_TOOLS_HOME="$AI_TOOLS_HOME" run bash "$INSTALL_SH"
}

# claude — сложнее остальных: различает «нет вовсе» и «сломанный лаунчер».

@test "claude: работает — не переустанавливаем, curl не зовём" {
  mock_bin claude 0
  cat > "$MOCK_BIN_DIR/claude" <<'EOF'
#!/usr/bin/env bash
[ "$1" = "--version" ] && exit 0
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/claude"
  run_install
  assert_output --partial "claude already installed"
  run cat "$MOCK_CALLS_DIR/curl.log"
  refute_output --partial "claude.ai"
}

@test "claude: бинарь есть, но --version падает (broken launcher) — переустанавливаем" {
  cat > "$MOCK_BIN_DIR/claude" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
  chmod +x "$MOCK_BIN_DIR/claude"
  run_install
  assert_output --partial "broken claude launcher"
}

@test "claude: нет вовсе — устанавливаем" {
  rm -f "$MOCK_BIN_DIR/claude"
  run_install
  assert_output --partial "Installing claude"
}

# dsh — двухшаговый npm install с fallback.

@test "dsh: уже работает — не переустанавливаем" {
  cat > "$MOCK_BIN_DIR/dsh" <<'EOF'
#!/usr/bin/env bash
[ "$1" = "--version" ] && { echo "1.0.0"; exit 0; }
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/dsh"
  run_install
  assert_output --partial "dsh already installed"
}

@test "dsh: нет — пробует npm с --allow-scripts" {
  rm -f "$MOCK_BIN_DIR/dsh"
  run_install
  assert_output --partial "Installing dsh"
}

# graphify — version-mismatch warning со скиллом платформы.

@test "graphify: версия CLI расходится со скиллом платформы — предупреждение" {
  mkdir -p "$PLATFORM_ROOT/skills/graphify"
  # .graphify_version уже реально существует в платформе — подменять не будем,
  # только читаем его, чтобы собрать заведомо несовпадающий мок.
  local want
  want="$(cat "$PLATFORM_ROOT/skills/graphify/.graphify_version" 2>/dev/null || echo "0.0.0")"
  cat > "$MOCK_BIN_DIR/graphify" <<EOF
#!/usr/bin/env bash
[ "\$1" = "--version" ] && { echo "graphify 999.999.999"; exit 0; }
[ "\$1" = "install" ] && exit 0
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/graphify"
  [ "$want" = "999.999.999" ] && skip "версия платформы случайно совпала с моком"
  run_install
  assert_output --partial "graphify в платформе собран под"
}

@test "graphify: версии совпадают — без предупреждения" {
  mkdir -p "$PLATFORM_ROOT/skills/graphify"
  local want
  want="$(cat "$PLATFORM_ROOT/skills/graphify/.graphify_version" 2>/dev/null)"
  [ -n "$want" ] || skip "нет .graphify_version в платформе"
  cat > "$MOCK_BIN_DIR/graphify" <<EOF
#!/usr/bin/env bash
[ "\$1" = "--version" ] && { echo "graphify $want"; exit 0; }
[ "\$1" = "install" ] && exit 0
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/graphify"
  run_install
  refute_output --partial "собран под"
}

# Однотипные gate'ы остальных инструментов — по одному тесту на «уже стоит».

@test "opencode: уже установлен — не переустанавливаем" {
  mock_bin opencode 0 "opencode 1.0.0"
  run_install
  assert_output --partial "opencode already installed"
}

@test "codex: уже установлен — не переустанавливаем" {
  mock_bin codex 0 "codex 1.0.0"
  run_install
  assert_output --partial "codex already installed"
}

@test "hermes: работает — не переустанавливаем" {
  cat > "$MOCK_BIN_DIR/hermes" <<'EOF'
#!/usr/bin/env bash
[ "$1" = "--version" ] && { echo "1.0.0"; exit 0; }
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/hermes"
  run_install
  assert_output --partial "hermes already installed"
}

@test "hermes: бинарь сломан — чистит venv и переустанавливает" {
  cat > "$MOCK_BIN_DIR/hermes" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
  chmod +x "$MOCK_BIN_DIR/hermes"
  run_install
  assert_output --partial "Found broken hermes"
}

@test "strix: уже установлен — не переустанавливаем" {
  mock_bin strix 0 "strix 1.0.0"
  run_install
  assert_output --partial "strix already installed"
}

@test "skaffold: уже установлен (executable в AI_TOOLS_HOME/bin) — не переустанавливаем" {
  mkdir -p "$AI_TOOLS_HOME/bin"
  printf '#!/usr/bin/env bash\ntrue\n' > "$AI_TOOLS_HOME/bin/skaffold"
  chmod +x "$AI_TOOLS_HOME/bin/skaffold"
  run_install
  assert_output --partial "skaffold already installed"
}

@test "uv: уже установлен (executable в AI_TOOLS_HOME/bin) — не переустанавливаем" {
  mkdir -p "$AI_TOOLS_HOME/bin"
  printf '#!/usr/bin/env bash\ntrue\n' > "$AI_TOOLS_HOME/bin/uv"
  chmod +x "$AI_TOOLS_HOME/bin/uv"
  run_install
  assert_output --partial "uv already installed"
}
