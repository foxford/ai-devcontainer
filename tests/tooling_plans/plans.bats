#!/usr/bin/env bats
# tooling/plans.sh — чистая файловая логика, без внешних side-effects.

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/fixtures'

  PLATFORM_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  PLANS_SH="$PLATFORM_ROOT/tooling/plans.sh"
  REPO_DIR="$(make_repo_fixture)"
}

teardown() {
  cleanup_fixture "$REPO_DIR"
}

make_task() {
  local rel="$1" status="$2" updated="$3"
  local dir="$REPO_DIR/plans/$rel"
  mkdir -p "$dir"
  {
    echo "---"
    echo "status: $status"
    [ -n "$updated" ] && echo "updated: $updated"
    echo "---"
    echo "# task"
  } > "$dir/task.md"
}

@test "нет plans/ — сообщение и exit 0" {
  REPO_ROOT="$REPO_DIR" run bash "$PLANS_SH"
  assert_success
  assert_output --partial "каталог планов"
}

@test "plans/ есть, но пуст — exit 0" {
  mkdir -p "$REPO_DIR/plans"
  REPO_ROOT="$REPO_DIR" run bash "$PLANS_SH"
  assert_success
  assert_output --partial "нет ни одной задачи"
}

@test "открытая задача попадает в список без --all" {
  make_task "2026/my-task" "in_progress" "2026-01-15"
  REPO_ROOT="$REPO_DIR" run bash "$PLANS_SH"
  assert_success
  assert_output --partial "2026/my-task"
  assert_output --partial "in_progress"
}

@test "закрытая задача скрыта без --all, видна с --all" {
  make_task "2026/done-task" "done" "2026-01-10"
  REPO_ROOT="$REPO_DIR" run bash "$PLANS_SH"
  assert_success
  refute_output --partial "done-task"
  assert_output --partial "закрытых: 1"

  REPO_ROOT="$REPO_DIR" run bash "$PLANS_SH" --all
  assert_success
  assert_output --partial "done-task"
}

@test "dropped тоже считается закрытой" {
  make_task "2026/dropped-task" "dropped" "2026-01-10"
  REPO_ROOT="$REPO_DIR" run bash "$PLANS_SH"
  assert_success
  refute_output --partial "dropped-task"
}

@test "task.md приоритетнее epic.md в одном каталоге" {
  local dir="$REPO_DIR/plans/2026/both"
  mkdir -p "$dir"
  printf -- '---\nstatus: in_progress\nupdated: 2026-01-01\n---\n' > "$dir/epic.md"
  printf -- '---\nstatus: in_progress\nupdated: 2026-01-02\n---\n' > "$dir/task.md"
  REPO_ROOT="$REPO_DIR" run bash "$PLANS_SH"
  assert_success
  assert_output --partial "2026-01-02"
  refute_output --partial "epic.md"
}

@test "задача без updated/created всплывает наверх (0000-00-00)" {
  make_task "2026/no-date" "in_progress" ""
  make_task "2026/with-date" "in_progress" "2026-06-01"
  REPO_ROOT="$REPO_DIR" run bash "$PLANS_SH"
  assert_success
  # no-date (0000-00-00) должна идти раньше with-date в отсортированном выводе
  no_date_line="$(echo "$output" | grep -n 'no-date' | head -1 | cut -d: -f1)"
  with_date_line="$(echo "$output" | grep -n 'with-date' | head -1 | cut -d: -f1)"
  [ "$no_date_line" -lt "$with_date_line" ]
}

@test "задача вне plans/<год>/ помечается как legacy" {
  local dir="$REPO_DIR/plans/flat-task"
  mkdir -p "$dir"
  printf -- '---\nstatus: in_progress\nupdated: 2026-01-01\n---\n' > "$dir/task.md"
  REPO_ROOT="$REPO_DIR" run bash "$PLANS_SH"
  assert_success
  assert_output --partial "вне plans/<год>/"
  assert_output --partial "на старой схеме"
}

@test "задача в archive/ помечается меткой archive" {
  local dir="$REPO_DIR/plans/archive/old-task"
  mkdir -p "$dir"
  printf -- '---\nstatus: done\nupdated: 2025-01-01\n---\n' > "$dir/task.md"
  REPO_ROOT="$REPO_DIR" run bash "$PLANS_SH" --all
  assert_success
  assert_output --partial "archive/"
}

@test "неверный флаг — ошибка использования, exit 1" {
  mkdir -p "$REPO_DIR/plans"
  REPO_ROOT="$REPO_DIR" run bash "$PLANS_SH" --bogus
  assert_failure
  assert_output --partial "Использование"
}
