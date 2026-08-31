#!/usr/bin/env bats
# tooling/skill.sh — cmd_list/status/fork/unfork/migrate/sync. Изолируем от
# tooling/wire-agent-skills.sh (протестирован отдельно) заглушкой по тому же
# пути — cmd_sync/fork/unfork/migrate её вызывают, мы проверяем факт вызова.

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/fixtures'

  REAL_PLATFORM_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  SKILL_SH="$REAL_PLATFORM_ROOT/tooling/skill.sh"

  REPO_DIR="$(make_repo_fixture)"
  mkdir -p "$REPO_DIR/.agents"
  PLAT_SKILLS_DIR="$(mktemp -d)"

  PLATFORM_FIXTURE="$(mktemp -d)"
  mkdir -p "$PLATFORM_FIXTURE/tooling" "$PLATFORM_FIXTURE/skeleton" "$PLATFORM_FIXTURE/skills"
  WIRE_LOG="$(mktemp)"
  cat > "$PLATFORM_FIXTURE/tooling/wire-agent-skills.sh" <<EOF
#!/usr/bin/env bash
echo "called REPO_ROOT=\$REPO_ROOT" >> "$WIRE_LOG"
EOF
  chmod +x "$PLATFORM_FIXTURE/tooling/wire-agent-skills.sh"
}

teardown() {
  cleanup_fixture "$REPO_DIR"
  rm -rf "$PLAT_SKILLS_DIR" "$PLATFORM_FIXTURE"
  rm -f "$WIRE_LOG"
}

make_platform_skill() {
  local name="$1" file="${2:-SKILL.md}" content="${3:-platform version}"
  mkdir -p "$(dirname "$PLAT_SKILLS_DIR/$name/$file")"
  echo "$content" > "$PLAT_SKILLS_DIR/$name/$file"
}

run_skill() {
  REPO_ROOT="$REPO_DIR" AI_DEVCONTAINER_HOME="$PLATFORM_FIXTURE" \
    AI_DEVCONTAINER_SKILLS="$PLAT_SKILLS_DIR" run bash "$SKILL_SH" "$@"
}

# ── cmd_list ──────────────────────────────────────────────────

@test "list: платформенный скилл без форка" {
  make_platform_skill "demo"
  run_skill list
  assert_success
  assert_output --partial "demo"
  assert_output --partial "платформа"
  refute_output --partial "форк"
}

@test "list: платформенный скилл с форком показывает число файлов" {
  make_platform_skill "demo"
  mkdir -p "$REPO_DIR/.agents/skills/demo"
  echo "forked" > "$REPO_DIR/.agents/skills/demo/SKILL.md"
  run_skill list
  assert_success
  assert_output --partial "платформа + форк (1 файл"
}

@test "list: чисто проектный скилл помечен «только проект»" {
  mkdir -p "$REPO_DIR/.agents/skills/own-thing"
  echo "x" > "$REPO_DIR/.agents/skills/own-thing/SKILL.md"
  run_skill list
  assert_success
  assert_output --partial "только проект"
}

# ── cmd_status ────────────────────────────────────────────────

@test "status: форков нет" {
  make_platform_skill "demo"
  run_skill status
  assert_success
  assert_output --partial "форков нет"
}

@test "status: форк актуален (база совпадает с платформой)" {
  make_platform_skill "demo" "SKILL.md" "v1"
  mkdir -p "$REPO_DIR/.agents/skills/demo" "$REPO_DIR/.agents/skills-base/demo"
  echo "v1-forked" > "$REPO_DIR/.agents/skills/demo/SKILL.md"
  echo "v1" > "$REPO_DIR/.agents/skills-base/demo/SKILL.md"
  run_skill status
  assert_success
  assert_output --partial "форк актуален"
}

@test "status: платформа уехала (база отличается от текущей платформы)" {
  make_platform_skill "demo" "SKILL.md" "v2"
  mkdir -p "$REPO_DIR/.agents/skills/demo" "$REPO_DIR/.agents/skills-base/demo"
  echo "v1-forked" > "$REPO_DIR/.agents/skills/demo/SKILL.md"
  echo "v1" > "$REPO_DIR/.agents/skills-base/demo/SKILL.md"
  run_skill status
  assert_success
  assert_output --partial "платформа уехала"
}

@test "status: форк без базы" {
  make_platform_skill "demo" "SKILL.md" "v1"
  mkdir -p "$REPO_DIR/.agents/skills/demo"
  echo "v1-forked" > "$REPO_DIR/.agents/skills/demo/SKILL.md"
  run_skill status
  assert_success
  assert_output --partial "форк без базы"
}

@test "status: своё (на платформе нет)" {
  mkdir -p "$REPO_DIR/.agents/skills/own-thing"
  echo "x" > "$REPO_DIR/.agents/skills/own-thing/SKILL.md"
  run_skill status
  assert_success
  assert_output --partial "своё (на платформе нет)"
}

# ── cmd_fork / cmd_unfork ─────────────────────────────────────

@test "fork: без файла форкает SKILL.md по умолчанию, вызывает sync" {
  make_platform_skill "demo" "SKILL.md" "v1"
  run_skill fork demo
  assert_success
  assert_output --partial "форк: .agents/skills/demo/SKILL.md"

  [ -f "$REPO_DIR/.agents/skills/demo/SKILL.md" ]
  [ -f "$REPO_DIR/.agents/skills-base/demo/SKILL.md" ]
  run cat "$WIRE_LOG"
  assert_output --partial "called"
}

@test "fork: несуществующий платформенный скилл — ошибка" {
  run_skill fork nonexistent
  assert_failure
  assert_output --partial "нет платформенного скилла"
}

@test "fork: конкретный файл, не по умолчанию" {
  make_platform_skill "demo" "scripts/live.mjs" "code"
  make_platform_skill "demo" "SKILL.md" "v1"
  run_skill fork demo scripts/live.mjs
  assert_success
  [ -f "$REPO_DIR/.agents/skills/demo/scripts/live.mjs" ]
  [ ! -e "$REPO_DIR/.agents/skills/demo/SKILL.md" ]
}

@test "fork: повторный форк того же файла — пропускаем" {
  make_platform_skill "demo" "SKILL.md" "v1"
  run_skill fork demo
  assert_success
  run_skill fork demo
  assert_success
  assert_output --partial "уже форкнут"
}

@test "unfork: конкретный файл убирает только его" {
  make_platform_skill "demo" "SKILL.md" "v1"
  run_skill fork demo
  assert_success
  run_skill unfork demo SKILL.md
  assert_success
  assert_output --partial "вернул на платформенную версию: demo/SKILL.md"
  [ ! -e "$REPO_DIR/.agents/skills/demo/SKILL.md" ]
}

@test "unfork: без файла убирает весь скилл" {
  make_platform_skill "demo" "SKILL.md" "v1"
  run_skill fork demo
  assert_success
  run_skill unfork demo
  assert_success
  assert_output --partial "вернул на платформенную версию весь скилл: demo"
  [ ! -d "$REPO_DIR/.agents/skills/demo" ]
}

# ── cmd_migrate ───────────────────────────────────────────────

@test "migrate: файл идентичен платформе — выкидывается" {
  make_platform_skill "demo" "SKILL.md" "same content"
  mkdir -p "$REPO_DIR/.agents/skills/demo"
  echo "same content" > "$REPO_DIR/.agents/skills/demo/SKILL.md"
  run_skill migrate
  assert_success
  assert_output --partial "выкинуто копий 1"
  [ ! -e "$REPO_DIR/.agents/skills/demo/SKILL.md" ]
}

@test "migrate: файл отличается — остаётся форком, база фиксируется" {
  make_platform_skill "demo" "SKILL.md" "platform content"
  mkdir -p "$REPO_DIR/.agents/skills/demo"
  echo "custom content" > "$REPO_DIR/.agents/skills/demo/SKILL.md"
  run_skill migrate
  assert_success
  assert_output --partial "оставлено форков 1"
  [ -f "$REPO_DIR/.agents/skills/demo/SKILL.md" ]
  [ -f "$REPO_DIR/.agents/skills-base/demo/SKILL.md" ]
}

@test "migrate: свой скилл (нет на платформе) не трогается" {
  mkdir -p "$REPO_DIR/.agents/skills/own-thing"
  echo "x" > "$REPO_DIR/.agents/skills/own-thing/SKILL.md"
  run_skill migrate
  assert_success
  assert_output --partial "своих скиллов 1"
  [ -f "$REPO_DIR/.agents/skills/own-thing/SKILL.md" ]
}

# ── .gitignore ────────────────────────────────────────────────

@test "sync: дописывает недостающие строки в .gitignore один раз" {
  make_platform_skill "demo"
  echo "node_modules/" > "$REPO_DIR/.gitignore"
  run_skill sync
  assert_success
  run cat "$REPO_DIR/.gitignore"
  assert_output --partial "AGENTS.skills.md"

  run_skill sync
  assert_success
  run bash -c "grep -c 'AGENTS.skills.md' '$REPO_DIR/.gitignore'"
  assert_output "1"
}

# ── диспетчер ─────────────────────────────────────────────────

@test "неизвестная команда — ошибка" {
  run_skill bogus-command
  assert_failure
  assert_output --partial "Неизвестная команда"
}
