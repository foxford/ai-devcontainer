#!/usr/bin/env bats
# bin/ai-devcontainer — cmd_ensure_image / cmd_update / host_only.

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/fixtures'
  load '../helpers/mocks'

  BIN="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)/bin/ai-devcontainer"
  PLATFORM_FIXTURE="$(make_platform_fixture)"
  mocks_init
}

teardown() {
  chmod -R +w "$PLATFORM_FIXTURE" 2>/dev/null || true
  cleanup_fixture "$PLATFORM_FIXTURE"
  mocks_cleanup
}

run_bin() {
  AI_DEVCONTAINER_HOME="$PLATFORM_FIXTURE" run bash "$BIN" "$@"
}

@test "ensure-image: заблокировано, если PLATFORM_ROOT read-only" {
  chmod -w "$PLATFORM_FIXTURE"
  run_bin ensure-image
  assert_failure
  assert_output --partial "доступна только на хосте"
}

@test "ensure-image: образ актуален (label совпадает) — не пересобирает" {
  local key
  key="$(cat "$PLATFORM_FIXTURE/Dockerfile" "$PLATFORM_FIXTURE/tooling/setup.sh" | sha256sum | cut -c1-12)"
  mock_bin docker 0 ""
  cat > "$MOCK_BIN_DIR/docker" <<EOF
#!/usr/bin/env bash
echo "\$@" >> "$MOCK_CALLS_DIR/docker.log"
if [ "\$1" = "image" ] && [ "\$2" = "inspect" ]; then
  echo "$key"
  exit 0
fi
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/docker"

  run_bin ensure-image
  assert_success
  assert_output --partial "актуален"
  run cat "$MOCK_CALLS_DIR/docker.log"
  refute_output --partial "build"
}

@test "ensure-image: образа нет — собирает" {
  cat > "$MOCK_BIN_DIR/docker" <<EOF
#!/usr/bin/env bash
echo "\$@" >> "$MOCK_CALLS_DIR/docker.log"
if [ "\$1" = "image" ] && [ "\$2" = "inspect" ]; then
  exit 1
fi
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/docker"

  run_bin ensure-image
  assert_success
  assert_output --partial "Готово: dev-base:local"
  run cat "$MOCK_CALLS_DIR/docker.log"
  assert_output --partial "build"
}

@test "update: PLATFORM_ROOT read-only — делегирует в sync" {
  # Заглушки для sub-скриптов, которые cmd_sync зовёт — до chmod -w.
  printf '#!/usr/bin/env bash\ntrue\n' > "$PLATFORM_FIXTURE/tooling/skill.sh"
  printf '#!/usr/bin/env bash\ntrue\n' > "$PLATFORM_FIXTURE/tooling/wire-mcp.sh"
  chmod +x "$PLATFORM_FIXTURE/tooling/skill.sh" "$PLATFORM_FIXTURE/tooling/wire-mcp.sh"
  REPO_DIR="$(make_repo_fixture)"
  chmod -w "$PLATFORM_FIXTURE"

  AI_DEVCONTAINER_HOME="$PLATFORM_FIXTURE" REPO_ROOT="$REPO_DIR" run bash "$BIN" update
  assert_success
  assert_output --partial "read-only"
  cleanup_fixture "$REPO_DIR"
}

@test "update: writable, не git-клон — предупреждает, но пересобирает" {
  cat > "$MOCK_BIN_DIR/docker" <<EOF
#!/usr/bin/env bash
[ "\$1" = "image" ] && [ "\$2" = "inspect" ] && exit 1
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/docker"
  run_bin update
  assert_success
  assert_output --partial "не git-клон, пропускаю pull"
}
