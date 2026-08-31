#!/usr/bin/env bats
# tooling/wire-agent-skills.sh — overlay-сборка скиллов. Чистая файловая
# логика (никаких docker/git/npm), изоляция через AI_DEVCONTAINER_SKILLS /
# CODEX_HOME / REPO_ROOT / AI_DEVCONTAINER_DOCS (последний — чтобы не задеть
# реальный docs/ платформы через вызов соседнего wire-docs.sh в конце).

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/fixtures'

  PLATFORM_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  WIRE_SKILLS="$PLATFORM_ROOT/tooling/wire-agent-skills.sh"

  REPO_DIR="$(make_repo_fixture)"
  PLAT_SKILLS_DIR="$(mktemp -d)"
  export CODEX_HOME="$(mktemp -d)"
  EMPTY_DOCS="$(mktemp -d)"
}

teardown() {
  cleanup_fixture "$REPO_DIR"
  rm -rf "$PLAT_SKILLS_DIR" "$CODEX_HOME" "$EMPTY_DOCS"
}

make_platform_skill() {
  local name="$1" desc="${2:-test skill}"
  mkdir -p "$PLAT_SKILLS_DIR/$name"
  cat > "$PLAT_SKILLS_DIR/$name/SKILL.md" <<EOF
---
name: $name
description: $desc
---
Body.
EOF
}

run_wire_skills() {
  REPO_ROOT="$REPO_DIR" AI_DEVCONTAINER_SKILLS="$PLAT_SKILLS_DIR" \
    AI_DEVCONTAINER_DOCS="$EMPTY_DOCS" run bash "$WIRE_SKILLS"
}

@test "нет скиллов ни на одном слое — предупреждение, exit 0" {
  run_wire_skills
  assert_success
  assert_output --partial "ни одного скилла не найдено"
}

@test "один платформенный скилл собирается в .claude/skills" {
  make_platform_skill "demo-skill"
  run_wire_skills
  assert_success
  assert_output --partial "Собрано скиллов: 1"

  [ -f "$REPO_DIR/.claude/skills/demo-skill/SKILL.md" ]
  [ -L "$REPO_DIR/.claude/skills/demo-skill/SKILL.md" ]
}

@test "проза — симлинк, код — копия" {
  make_platform_skill "demo-skill"
  echo "console.log(1)" > "$PLAT_SKILLS_DIR/demo-skill/script.mjs"
  run_wire_skills
  assert_success

  [ -L "$REPO_DIR/.claude/skills/demo-skill/SKILL.md" ]
  [ ! -L "$REPO_DIR/.claude/skills/demo-skill/script.mjs" ]
  [ -f "$REPO_DIR/.claude/skills/demo-skill/script.mjs" ]
}

@test "проектный форк SKILL.md перекрывает платформенный, засчитан в индексе" {
  make_platform_skill "demo-skill"
  mkdir -p "$REPO_DIR/.agents/skills/demo-skill"
  cat > "$REPO_DIR/.agents/skills/demo-skill/SKILL.md" <<'EOF'
---
name: demo-skill
description: forked description
---
Forked body.
EOF
  run_wire_skills
  assert_success

  run cat "$REPO_DIR/.claude/skills/demo-skill/SKILL.md"
  assert_output --partial "Forked body."

  run cat "$REPO_DIR/AGENTS.skills.md"
  assert_output --partial "форк: 1 файл"
  assert_output --partial "forked description"
}

@test "чисто проектный скилл (нет на платформе) помечен как проектный" {
  mkdir -p "$REPO_DIR/.agents/skills/my-own-skill"
  cat > "$REPO_DIR/.agents/skills/my-own-skill/SKILL.md" <<'EOF'
---
name: my-own-skill
description: own thing
---
EOF
  run_wire_skills
  assert_success

  run cat "$REPO_DIR/AGENTS.skills.md"
  assert_output --partial "проектный"
  assert_output --partial "own thing"
}

@test "скилл без SKILL.md ни на одном слое пропускается" {
  mkdir -p "$REPO_DIR/.agents/skills/empty-skill"
  echo "no skill.md here" > "$REPO_DIR/.agents/skills/empty-skill/notes.txt"
  run_wire_skills
  assert_success
  assert_output --partial "без SKILL.md"
  [ ! -d "$REPO_DIR/.claude/skills/empty-skill" ]
}

@test "Codex получает симлинк на собранное дерево" {
  make_platform_skill "demo-skill"
  run_wire_skills
  assert_success

  [ -L "$CODEX_HOME/skills/demo-skill" ]
  target="$(readlink "$CODEX_HOME/skills/demo-skill")"
  [ "$target" = "$REPO_DIR/.claude/skills/demo-skill" ]
}

@test "DSH: kebab-case имя получает симлинк, не kebab-case — предупреждение без симлинка" {
  make_platform_skill "good-skill"
  mkdir -p "$REPO_DIR/.agents/skills/BadSkill"
  cat > "$REPO_DIR/.agents/skills/BadSkill/SKILL.md" <<'EOF'
---
name: BadSkill
description: not kebab
---
EOF
  run_wire_skills
  assert_success
  assert_output --partial "не kebab-case"

  [ -L "$REPO_DIR/.dsh/skills/good-skill" ]
  [ ! -e "$REPO_DIR/.dsh/skills/BadSkill" ]
}

@test "устаревший скилл вычищается из .claude/skills при повторной сборке" {
  make_platform_skill "will-be-removed"
  run_wire_skills
  assert_success
  [ -d "$REPO_DIR/.claude/skills/will-be-removed" ]

  rm -rf "$PLAT_SKILLS_DIR/will-be-removed"
  make_platform_skill "stays"
  run_wire_skills
  assert_success

  [ ! -d "$REPO_DIR/.claude/skills/will-be-removed" ]
  [ -d "$REPO_DIR/.claude/skills/stays" ]
}

@test "AGENTS.md без файла — ссылку на индекс не ставим" {
  make_platform_skill "demo-skill"
  run_wire_skills
  assert_success
  assert_output --partial "ссылку на индекс не ставлю"
}

@test "AGENTS.md есть без ссылки — добавляется секция «Тех-скиллы»" {
  make_platform_skill "demo-skill"
  echo "# Проект" > "$REPO_DIR/AGENTS.md"
  run_wire_skills
  assert_success
  assert_output --partial "поставил ссылку на индекс"

  run cat "$REPO_DIR/AGENTS.md"
  assert_output --partial "## Тех-скиллы"
  assert_output --partial "AGENTS.skills.md"
}

@test "AGENTS.md уже со ссылкой — не трогаем" {
  make_platform_skill "demo-skill"
  printf '# Проект\n\nСм. [AGENTS.skills.md](AGENTS.skills.md).\n' > "$REPO_DIR/AGENTS.md"
  run_wire_skills
  assert_success
  assert_output --partial "AGENTS.md трогать не нужно"
}

@test "повторный прогон идемпотентен" {
  make_platform_skill "demo-skill"
  run_wire_skills
  assert_success
  run_wire_skills
  assert_success
  assert_output --partial "Собрано скиллов: 1"
  [ -f "$REPO_DIR/.claude/skills/demo-skill/SKILL.md" ]
}
