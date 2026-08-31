#!/usr/bin/env bats
# tooling/setup.sh — build-time настройка Node/pnpm внутри образа. Линейный
# скрипт (main() выполняется сразу) — тестируем через полные end-to-end
# прогоны с REPO_ROOT-фикстурой и PATH-моками asdf/npm/corepack/pnpm.
# node -e используем РЕАЛЬНЫЙ (нужен настоящий JS-движок для чтения
# package.json), а `node --version` подменяем оберткой.

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/fixtures'
  load '../helpers/mocks'

  PLATFORM_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  SETUP_SH="$PLATFORM_ROOT/tooling/setup.sh"
  # command -v node может резолвиться в asdf-шим, а не в реальный бинарь — шим
  # при exec внутри снова зовёт `asdf`, и с нашим мок-asdf в PATH это ловит
  # чужой мок. asdf which резолвит шим до настоящего пути ДО подмены PATH.
  REAL_NODE="$(asdf which node 2>/dev/null || command -v node)"

  REPO_DIR="$(mktemp -d)"
  echo "nodejs 26.5.0" > "$REPO_DIR/.tool-versions"
  echo '{"packageManager": "pnpm@10.25.0"}' > "$REPO_DIR/package.json"

  mocks_init
}

teardown() {
  rm -rf "$REPO_DIR"
  mocks_cleanup
}

node_mock() {
  local version="$1"
  cat > "$MOCK_BIN_DIR/node" <<EOF
#!/usr/bin/env bash
if [ "\$1" = "--version" ]; then echo "v$version"; exit 0; fi
exec "$REAL_NODE" "\$@"
EOF
  chmod +x "$MOCK_BIN_DIR/node"
}

run_setup() {
  REPO_ROOT="$REPO_DIR" run bash "$SETUP_SH" "$@"
}

@test "--help выводит usage, exit 0" {
  run bash "$SETUP_SH" --help
  assert_success
  assert_output --partial "Использование"
  assert_output --partial "--env-only"
}

@test "неизвестный аргумент — ошибка" {
  run bash "$SETUP_SH" --bogus
  assert_failure
  assert_output --partial "Неизвестный аргумент"
}

@test "нет nodejs в .tool-versions — падает с понятной ошибкой" {
  echo "ruby 3.4.4" > "$REPO_DIR/.tool-versions"
  node_mock "26.5.0"
  run_setup --env-only
  assert_failure
  assert_output --partial "Не удалось определить версию Node.js"
}

@test "нужная версия Node уже активна — asdf не зовём" {
  node_mock "26.5.0"
  mock_bin asdf 1 ""   # если бы позвали asdf — тест это поймает
  cat > "$MOCK_BIN_DIR/corepack" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/corepack"
  cat > "$MOCK_BIN_DIR/npm" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/npm"
  # pnpm сам иногда asdf-шим — версия в финальном success-сообщении не важна
  # для этого теста, но вызов должен остаться внутри мока, не уйти в шим.
  mock_bin pnpm 0 "10.25.0"
  run_setup --env-only
  assert_success
  assert_output --partial "Node.js 26.5.0 уже установлен"
  [ ! -s "$MOCK_CALLS_DIR/asdf.log" ]
}

@test "версия Node не совпадает — ставит через asdf" {
  node_mock "20.0.0"
  cat > "$MOCK_BIN_DIR/asdf" <<'EOF'
#!/usr/bin/env bash
case "$1 $2" in
  "plugin list") echo "nodejs" ;;
  "plugin add") ;;
  install) ;;
  reshim) ;;
esac
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/asdf"
  cat > "$MOCK_BIN_DIR/corepack" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/corepack"
  cat > "$MOCK_BIN_DIR/npm" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/npm"
  mock_bin pnpm 0 "10.25.0"
  run_setup --env-only
  # Ставим ТРЕБУЕМУЮ версию (26.5.0 из .tool-versions), не текущую (20.0.0).
  assert_output --partial "Устанавливаю Node.js 26.5.0 через asdf"
}

@test "версия pnpm читается из package.json, corepack prepare зовётся с ней" {
  node_mock "26.5.0"
  cat > "$MOCK_BIN_DIR/corepack" <<EOF
#!/usr/bin/env bash
echo "\$@" >> "$MOCK_CALLS_DIR/corepack.log"
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/corepack"
  cat > "$MOCK_BIN_DIR/npm" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/npm"
  run_setup --env-only
  assert_success
  assert_output --partial "Требуемая версия pnpm: 10.25.0"
  run cat "$MOCK_CALLS_DIR/corepack.log"
  assert_output --partial "prepare pnpm@10.25.0"
}

@test "--env-only не запускает pnpm install" {
  node_mock "26.5.0"
  cat > "$MOCK_BIN_DIR/corepack" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/corepack"
  cat > "$MOCK_BIN_DIR/npm" <<EOF
#!/usr/bin/env bash
echo "\$@" >> "$MOCK_CALLS_DIR/npm.log"
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/npm"
  run_setup --env-only
  assert_success
  refute_output --partial "Устанавливаю зависимости проекта"
}

@test "без --env-only запускает pnpm install" {
  node_mock "26.5.0"
  cat > "$MOCK_BIN_DIR/corepack" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/corepack"
  cat > "$MOCK_BIN_DIR/npm" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/npm"
  cat > "$MOCK_BIN_DIR/pnpm" <<EOF
#!/usr/bin/env bash
echo "\$@" >> "$MOCK_CALLS_DIR/pnpm.log"
[ "\$1" = "--version" ] && { echo "10.25.0"; exit 0; }
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/pnpm"
  run_setup
  assert_success
  assert_output --partial "Устанавливаю зависимости проекта"
  run cat "$MOCK_CALLS_DIR/pnpm.log"
  assert_output --partial "install"
}
