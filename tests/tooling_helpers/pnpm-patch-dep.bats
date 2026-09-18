#!/usr/bin/env bats
# tooling/helpers/pnpm-patch-dep.sh — приоритет: JS-анализатор внутри
# (heredoc между PATCH_DEP_ANALYZER_EOF-маркерами) — самая богатая чистая
# логика во всём репозитории (mini-semver, capper detection, declarer scan).
# Извлекаем его из реального файла (не дублируем руками — дрейф иначе
# гарантирован) и гоняем `node analyzer.cjs PKG VER` напрямую на fixture
# node_modules/.pnpm/ дереве. Отдельно — несколько bash-обёрточных тестов
# с моком pnpm/npm (override dance).

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/mocks'

  PLATFORM_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  SCRIPT="$PLATFORM_ROOT/tooling/helpers/pnpm-patch-dep.sh"

  ANALYZER="$(mktemp --suffix=.cjs)"
  awk '/^cat > "\$ANALYZER" <<.PATCH_DEP_ANALYZER_EOF.$/{flag=1; next} /^PATCH_DEP_ANALYZER_EOF$/{flag=0; next} flag' \
    "$SCRIPT" > "$ANALYZER"
  [ -s "$ANALYZER" ] || fail "не удалось извлечь JS-анализатор из $SCRIPT — маркеры heredoc изменились?"

  PROJECT_DIR="$(mktemp -d)"
  mocks_init
}

teardown() {
  rm -f "$ANALYZER"
  rm -rf "$PROJECT_DIR"
  mocks_cleanup
}

# helper: node_modules/.pnpm/<store-dir>/node_modules/<name>/package.json
add_pnpm_store_pkg() {
  local store_dir="$1" name="$2" deps_json="$3"
  local d="$PROJECT_DIR/node_modules/.pnpm/$store_dir/node_modules/$name"
  mkdir -p "$d"
  echo "$deps_json" > "$d/package.json"
}

run_analyzer() {
  cd "$PROJECT_DIR" && run node "$ANALYZER" "$@"
}

# ── JS-анализатор напрямую ───────────────────────────────────

@test "analyzer: нет капперов — пустой вывод, exit 0" {
  add_pnpm_store_pkg "some-lib@1.0.0" "some-lib" '{"dependencies":{"lodash":"^4.17.0"}}'
  run_analyzer lodash 4.17.21
  assert_success
  assert_output ""
}

@test "analyzer: капер найден — имя, диапазон и причину показывает" {
  add_pnpm_store_pkg "old-lib@1.0.0" "old-lib" '{"dependencies":{"lodash":"^3.0.0"}}'
  run_analyzer lodash 4.17.21
  assert_success
  assert_output --partial "old-lib"
  assert_output --partial '"^3.0.0"'
  assert_output --partial "held below"
}

@test "analyzer: сам целевой пакет исключён из сканирования" {
  add_pnpm_store_pkg "lodash@3.0.0" "lodash" '{"dependencies":{"lodash":"^3.0.0"}}'
  run_analyzer lodash 4.17.21
  assert_success
  assert_output ""
}

@test "analyzer: peer-dependency помечается (peer)" {
  add_pnpm_store_pkg "peer-lib@1.0.0" "peer-lib" '{"peerDependencies":{"lodash":"^3.0.0"}}'
  run_analyzer lodash 4.17.21
  assert_success
  assert_output --partial "(peer)"
}

@test "analyzer: declarer найден в корневом package.json — показывает, где объявлен" {
  add_pnpm_store_pkg "old-lib@1.0.0" "old-lib" '{"dependencies":{"lodash":"^3.0.0"}}'
  echo '{"name": "@acme/root", "dependencies": {"old-lib": "1.0.0"}}' > "$PROJECT_DIR/package.json"
  run_analyzer lodash 4.17.21
  assert_success
  assert_output --partial "declared in: @acme/root"
}

@test "analyzer: без деклараторов — подсказка pnpm why" {
  add_pnpm_store_pkg "old-lib@1.0.0" "old-lib" '{"dependencies":{"lodash":"^3.0.0"}}'
  run_analyzer lodash 4.17.21
  assert_success
  assert_output --partial "pnpm why old-lib"
}

@test "analyzer: node_modules/ и .git/ в дереве манифестов не сканируются" {
  add_pnpm_store_pkg "old-lib@1.0.0" "old-lib" '{"dependencies":{"lodash":"^3.0.0"}}'
  mkdir -p "$PROJECT_DIR/.git" "$PROJECT_DIR/node_modules/somewhere"
  echo '{"name": "should-not-appear", "dependencies": {"old-lib": "1.0.0"}}' > "$PROJECT_DIR/.git/package.json"
  run_analyzer lodash 4.17.21
  assert_success
  refute_output --partial "should-not-appear"
}

@test "analyzer: диапазон, разрешающий целевую версию, не капер" {
  add_pnpm_store_pkg "ok-lib@1.0.0" "ok-lib" '{"dependencies":{"lodash":"^4.0.0"}}'
  run_analyzer lodash 4.17.21
  assert_success
  assert_output ""
}

# ── bash-обёртка ──────────────────────────────────────────────

@test "usage: без аргументов — ошибка, exit 1" {
  run bash "$SCRIPT"
  assert_failure
  assert_output --partial "Usage"
}

@test "usage: больше двух аргументов — ошибка" {
  run bash "$SCRIPT" pkg 1.0.0 extra
  assert_failure
  assert_output --partial "Usage"
}

@test "нет пакета@major в lock file — no-op, exit 0" {
  cd "$PROJECT_DIR"
  echo '{}' > package.json
  echo "lockfileVersion: 6" > pnpm-lock.yaml
  run bash "$SCRIPT" lodash 4.17.21
  assert_success
  assert_output --partial "nothing to do"
}

@test "уже на целевой версии — no-op, pnpm не зовём" {
  cd "$PROJECT_DIR"
  echo '{}' > package.json
  printf "lodash@4.17.21:\n  resolution: {}\n" > pnpm-lock.yaml
  mock_bin pnpm 1 ""
  run bash "$SCRIPT" lodash 4.17.21
  assert_success
  assert_output --partial "Already at"
  [ ! -s "$MOCK_CALLS_DIR/pnpm.log" ]
}

@test "pnpm i --force падает — package.json восстановлен из бэкапа" {
  cd "$PROJECT_DIR"
  echo '{"name": "test-pkg"}' > package.json
  printf "lodash@4.17.20:\n  resolution: {}\n" > pnpm-lock.yaml
  mock_bin pnpm 1 "install failed"
  ORIGINAL="$(cat package.json)"
  run bash "$SCRIPT" lodash 4.17.21
  assert_failure
  assert_output --partial "restored from backup"
  [ "$(cat package.json)" = "$ORIGINAL" ]
  [ ! -f package.json.bak ]
}

@test "pnpm i (шаг 4, без override) падает — package.json тоже восстановлен" {
  cd "$PROJECT_DIR"
  echo '{"name": "test-pkg"}' > package.json
  printf "lodash@4.17.20:\n  resolution: {}\n" > pnpm-lock.yaml
  ORIGINAL="$(cat package.json)"
  cat > "$MOCK_BIN_DIR/pnpm" <<EOF
#!/usr/bin/env bash
echo "\$@" >> "$MOCK_CALLS_DIR/pnpm.log"
call="\$(grep -c . "$MOCK_CALLS_DIR/pnpm.log" 2>/dev/null || echo 0)"
if [ "\$call" -ge 2 ]; then exit 1; fi
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/pnpm"
  run bash "$SCRIPT" lodash 4.17.21
  assert_failure
  assert_output --partial "restored from backup"
  [ "$(cat package.json)" = "$ORIGINAL" ]
  [ ! -f package.json.bak ]
}

# ── куда кладётся override: package.json или pnpm-workspace.yaml ─────────────
# pnpm 11 перестал читать поле `pnpm` в package.json. Мок отражает ровно это:
# install патчит lock, только если override лежит в <where> — там, где ЭТОТ
# pnpm умеет его прочитать. Положили не туда → скрипт честно провалится.

# mock_pnpm <версия> <where>   where: workspace | package | sticky
mock_pnpm() {
  local version="$1" where="$2"
  cat > "$MOCK_BIN_DIR/pnpm" <<EOF
#!/usr/bin/env bash
if [ "\$1" = "--version" ]; then echo "$version"; exit 0; fi
echo "\$@" >> "$MOCK_CALLS_DIR/pnpm.log"
where="$where"
if [ "\$where" = "sticky" ]; then
  printf 'lodash@4.17.21:\n  resolution: {}\n' > pnpm-lock.yaml
  exit 0
fi
src="package.json"
[ "\$where" = "workspace" ] && src="pnpm-workspace.yaml"
if grep -q '4\.17\.21' "\$src" 2>/dev/null; then
  printf 'lodash@4.17.21:\n  resolution: {}\n' > pnpm-lock.yaml
else
  printf 'lodash@4.17.20:\n  resolution: {}\n' > pnpm-lock.yaml
fi
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/pnpm"
}

# проект с lodash@4.17.20 в lock; $1 — содержимое pnpm-workspace.yaml (пусто = файла нет)
setup_project() {
  cd "$PROJECT_DIR"
  echo '{"name": "test-pkg"}' > package.json
  printf "lodash@4.17.20:\n  resolution: {}\n" > pnpm-lock.yaml
  [ -n "${1:-}" ] && printf '%s' "$1" > pnpm-workspace.yaml
  return 0
}

@test "pnpm 11: override уходит в pnpm-workspace.yaml, package.json не тронут" {
  setup_project "packages:
  - 'apps/*'
"
  mock_pnpm 11.2.0 workspace
  run bash "$SCRIPT" lodash 4.17.21
  assert_success
  assert_output --partial "pnpm-workspace.yaml"
  grep -q "lodash@>=4.0.0 <4.17.21" pnpm-workspace.yaml
  grep -q "packages:" pnpm-workspace.yaml
  [ "$(cat package.json)" = '{"name": "test-pkg"}' ]
}

@test "pnpm 10: override остаётся в package.json, pnpm-workspace.yaml не трогаем" {
  setup_project "packages:
  - 'apps/*'
"
  WS_BEFORE="$(cat pnpm-workspace.yaml)"
  mock_pnpm 10.25.0 package
  run bash "$SCRIPT" lodash 4.17.21
  assert_success
  node -e "process.exit(require('$PROJECT_DIR/package.json').pnpm.overrides['lodash@>=4.0.0 <4.17.21'] === '4.17.21' ? 0 : 1)"
  [ "$(cat pnpm-workspace.yaml)" = "$WS_BEFORE" ]
}

@test "pnpm неизвестной версии (мок молчит) — деградируем в package.json" {
  setup_project
  mock_bin pnpm 0 ""
  run bash "$SCRIPT" lodash 4.17.21
  # цвет режет подстроку пополам — сверяем по некрашеному хвосту строки
  assert_output --partial "(pnpm unknown)"
  assert_output --partial "Adding permanent override to package.json"
}

@test "pnpm 11: созданный на время pnpm-workspace.yaml удаляется, когда override не нужен" {
  setup_project
  mock_pnpm 11.2.0 sticky
  run bash "$SCRIPT" lodash 4.17.21
  assert_success
  assert_output --partial "all 1 entries patched"
  [ ! -f pnpm-workspace.yaml ]
}

@test "pnpm 11: чужие записи в overrides не страдают" {
  setup_project "packages:
  - 'apps/*'

overrides:
  'left-pad@1': '1.3.0'
"
  WS_BEFORE="$(cat pnpm-workspace.yaml)"
  mock_pnpm 11.2.0 sticky
  run bash "$SCRIPT" lodash 4.17.21
  assert_success
  [ "$(cat pnpm-workspace.yaml)" = "$WS_BEFORE" ]
}

@test "pnpm 11: pnpm i падает — pnpm-workspace.yaml восстановлен байт-в-байт" {
  setup_project "packages:
  - 'apps/*'
"
  WS_BEFORE="$(cat pnpm-workspace.yaml)"
  cat > "$MOCK_BIN_DIR/pnpm" <<'EOF'
#!/usr/bin/env bash
[ "$1" = "--version" ] && { echo "11.2.0"; exit 0; }
echo "install failed"
exit 1
EOF
  chmod +x "$MOCK_BIN_DIR/pnpm"
  run bash "$SCRIPT" lodash 4.17.21
  assert_failure
  assert_output --partial "pnpm-workspace.yaml restored from backup"
  [ "$(cat pnpm-workspace.yaml)" = "$WS_BEFORE" ]
  [ ! -f pnpm-workspace.yaml.bak ]
}

@test "pnpm 11: мёртвые pnpm.overrides в package.json — предупреждение" {
  setup_project
  echo '{"name":"t","pnpm":{"overrides":{"left-pad":"1.3.0"}}}' > package.json
  mock_pnpm 11.2.0 sticky
  run bash "$SCRIPT" lodash 4.17.21
  assert_success
  assert_output --partial "this pnpm ignores them"
}
