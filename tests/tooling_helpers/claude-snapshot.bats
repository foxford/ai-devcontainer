#!/usr/bin/env bats
# tooling/helpers/claude-snapshot.sh — чистая файловая логика (tar/find/date),
# без docker/git/npm. python3 в cmd_retention реальный (опционален в коде,
# не идёт под нож по плану).

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/fixtures'

  PLATFORM_ROOT="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  SNAPSHOT_SH="$PLATFORM_ROOT/tooling/helpers/claude-snapshot.sh"

  export CLAUDE_CONFIG_DIR="$(mktemp -d)"
}

teardown() {
  rm -rf "$CLAUDE_CONFIG_DIR"
}

run_snapshot() {
  run bash "$SNAPSHOT_SH" "$@"
}

@test "snapshot: нет projects/ — предупреждение" {
  run_snapshot
  assert_success
  assert_output --partial "нечего снимать"
}

@test "snapshot: создаёт архив с транскриптами" {
  mkdir -p "$CLAUDE_CONFIG_DIR/projects/some-project"
  echo '{"msg":"hi"}' > "$CLAUDE_CONFIG_DIR/projects/some-project/session.jsonl"
  run_snapshot
  assert_success
  assert_output --partial "снапшот:"

  count="$(find "$CLAUDE_CONFIG_DIR/transcript-backups" -name 'transcripts-*.tar.gz' | wc -l)"
  [ "$count" -eq 1 ]
}

@test "snapshot: ротация держит последние 30 архивов" {
  mkdir -p "$CLAUDE_CONFIG_DIR/projects/p" "$CLAUDE_CONFIG_DIR/transcript-backups"
  echo x > "$CLAUDE_CONFIG_DIR/projects/p/s.jsonl"
  # 32 фиктивных архива с разными mtime — ротация должна оставить 30.
  local i
  for i in $(seq 1 32); do
    f="$CLAUDE_CONFIG_DIR/transcript-backups/transcripts-fake$i.tar.gz"
    tar -czf "$f" -C "$CLAUDE_CONFIG_DIR" projects
    touch -d "2020-01-$((i % 28 + 1))" "$f"
  done
  run_snapshot
  assert_success

  count="$(find "$CLAUDE_CONFIG_DIR/transcript-backups" -name 'transcripts-*.tar.gz' | wc -l)"
  [ "$count" -eq 30 ]
}

@test "daily: нет .jsonl вообще — снапшот не снимается" {
  mkdir -p "$CLAUDE_CONFIG_DIR/projects"
  run_snapshot daily
  assert_success
  [ ! -d "$CLAUDE_CONFIG_DIR/transcript-backups" ]
}

@test "daily: есть .jsonl, архивов ещё нет — снимает" {
  mkdir -p "$CLAUDE_CONFIG_DIR/projects/p"
  echo x > "$CLAUDE_CONFIG_DIR/projects/p/s.jsonl"
  run_snapshot daily
  assert_success
  count="$(find "$CLAUDE_CONFIG_DIR/transcript-backups" -name 'transcripts-*.tar.gz' | wc -l)"
  [ "$count" -eq 1 ]
}

@test "daily: последний архив свежий — повторно не снимает" {
  mkdir -p "$CLAUDE_CONFIG_DIR/projects/p"
  echo x > "$CLAUDE_CONFIG_DIR/projects/p/s.jsonl"
  run_snapshot
  assert_success
  run_snapshot daily
  assert_success
  count="$(find "$CLAUDE_CONFIG_DIR/transcript-backups" -name 'transcripts-*.tar.gz' | wc -l)"
  [ "$count" -eq 1 ]
}

@test "list: нет архивов — сообщение" {
  run_snapshot list
  assert_success
  assert_output --partial "снапшотов ещё нет"
}

@test "list: перечисляет архивы" {
  mkdir -p "$CLAUDE_CONFIG_DIR/projects/p"
  echo x > "$CLAUDE_CONFIG_DIR/projects/p/s.jsonl"
  run_snapshot
  assert_success
  run_snapshot list
  assert_success
  assert_output --partial "transcripts-"
}

@test "restore: несуществующий файл — ошибка" {
  run_snapshot restore /nonexistent/archive.tar.gz
  assert_failure
  assert_output --partial "нет файла"
}

@test "restore: распаковывает архив рядом, не затирая projects/" {
  mkdir -p "$CLAUDE_CONFIG_DIR/projects/p"
  echo original > "$CLAUDE_CONFIG_DIR/projects/p/s.jsonl"
  run_snapshot
  assert_success
  archive="$(find "$CLAUDE_CONFIG_DIR/transcript-backups" -name '*.tar.gz' | head -1)"

  echo changed > "$CLAUDE_CONFIG_DIR/projects/p/s.jsonl"
  run_snapshot restore "$archive"
  assert_success
  assert_output --partial "распаковано в"

  run cat "$CLAUDE_CONFIG_DIR/projects/p/s.jsonl"
  assert_output "changed"   # оригинал в projects/ не тронут
}

@test "retention: добавляет cleanupPeriodDays, если ключа нет" {
  mkdir -p "$CLAUDE_CONFIG_DIR"
  echo '{}' > "$CLAUDE_CONFIG_DIR/settings.json"
  run_snapshot retention
  assert_success
  assert_output --partial "cleanupPeriodDays=3650"
  run cat "$CLAUDE_CONFIG_DIR/settings.json"
  assert_output --partial '"cleanupPeriodDays": 3650'
}

@test "retention: не трогает, если ключ уже выставлен человеком" {
  mkdir -p "$CLAUDE_CONFIG_DIR"
  echo '{"cleanupPeriodDays": 7}' > "$CLAUDE_CONFIG_DIR/settings.json"
  run_snapshot retention
  assert_success
  refute_output --partial "3650"
  run cat "$CLAUDE_CONFIG_DIR/settings.json"
  assert_output --partial '"cleanupPeriodDays": 7'
}

@test "неизвестная команда — ошибка" {
  run_snapshot bogus
  assert_failure
  assert_output --partial "неизвестная команда"
}
