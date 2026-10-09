#!/usr/bin/env bats

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/fixtures'
  load '../helpers/mocks'
  BIN="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)/bin/adc"
  PLATFORM_FIXTURE="$(make_platform_fixture)"
  mocks_init
  export UPGRADE_TRACE="$MOCK_CALLS_DIR/upgrade.log"
  export UPGRADE_EXIT=0
  git -C "$PLATFORM_FIXTURE" init -q -b v0
  git -C "$PLATFORM_FIXTURE" config user.name ADC-Test
  git -C "$PLATFORM_FIXTURE" config user.email fixture@example.invalid
  git -C "$PLATFORM_FIXTURE" add .
  git -C "$PLATFORM_FIXTURE" commit -qm 'legacy fixture'
  OLD_REV="$(git -C "$PLATFORM_FIXTURE" rev-parse HEAD)"
  git -C "$PLATFORM_FIXTURE" switch -qc main
  printf 'module fixture\n' > "$PLATFORM_FIXTURE/go.mod"
  mkdir "$PLATFORM_FIXTURE/runtime"
  printf '{}\n' > "$PLATFORM_FIXTURE/runtime/orca-lock.json"
  mkdir -p "$PLATFORM_FIXTURE/bin/adc/host"
  printf 'package host\n' > "$PLATFORM_FIXTURE/bin/adc/host/legacy_install.go"
  cat > "$PLATFORM_FIXTURE/install.sh" <<'EOF'
#!/bin/sh
printf '%s\n' "$AI_DEVCONTAINER_HOME" "$AI_DEVCONTAINER_REPO" "${ADC_HOME:-default}" > "$UPGRADE_TRACE"
exit "$UPGRADE_EXIT"
EOF
  git -C "$PLATFORM_FIXTURE" add .
  git -C "$PLATFORM_FIXTURE" commit -qm 'new bootstrap fixture'
  git -C "$PLATFORM_FIXTURE" switch -q v0
  git -C "$PLATFORM_FIXTURE" remote add origin "$PLATFORM_FIXTURE"
}

teardown() {
  cleanup_fixture "$PLATFORM_FIXTURE"
  mocks_cleanup
}

@test "update --branch main: calls new bootstrap without replacing the old tree or building dev-base" {
  mock_bin docker 99 'old Docker build must not run'
  AI_DEVCONTAINER_HOME="$PLATFORM_FIXTURE" run bash "$BIN" update --branch main
  assert_success
  run cat "$UPGRADE_TRACE"
  assert_line --index 0 "$PLATFORM_FIXTURE"
  assert_line --index 1 "$PLATFORM_FIXTURE"
  assert_line --index 2 default
  [ "$(git -C "$PLATFORM_FIXTURE" rev-parse HEAD)" = "$OLD_REV" ]
  [ ! -e "$PLATFORM_FIXTURE/go.mod" ]
  [ "$(mock_call_count docker)" = 0 ]
}

@test "update --branch main: bootstrap failure propagates and leaves old checkout unchanged" {
  export UPGRADE_EXIT=23
  AI_DEVCONTAINER_HOME="$PLATFORM_FIXTURE" run bash "$BIN" update --branch main
  assert_failure 23
  [ "$(git -C "$PLATFORM_FIXTURE" rev-parse HEAD)" = "$OLD_REV" ]
  [ ! -e "$PLATFORM_FIXTURE/go.mod" ]
}

@test "update: refuses an unknown branch before fetching or invoking an installer" {
  AI_DEVCONTAINER_HOME="$PLATFORM_FIXTURE" run bash "$BIN" update --branch other
  assert_failure
  assert_output --partial 'поддерживаются ветки v0 и main'
  [ ! -e "$UPGRADE_TRACE" ]
}

@test "update: default stays on v0 and builds the old image" {
  mock_bin docker 0
  AI_DEVCONTAINER_HOME="$PLATFORM_FIXTURE" run bash "$BIN" update
  assert_success
  [ ! -e "$UPGRADE_TRACE" ]
  [ "$(git -C "$PLATFORM_FIXTURE" branch --show-current)" = v0 ]
  run cat "$MOCK_CALLS_DIR/docker.log"
  assert_output --partial dev-base:local
}

@test "update --branch main: refuses an older main without the migration installer" {
  git -C "$PLATFORM_FIXTURE" switch -q main
  git -C "$PLATFORM_FIXTURE" rm -q bin/adc/host/legacy_install.go
  git -C "$PLATFORM_FIXTURE" commit -qm 'main without migration helper'
  git -C "$PLATFORM_FIXTURE" switch -q v0
  AI_DEVCONTAINER_HOME="$PLATFORM_FIXTURE" run bash "$BIN" update --branch main
  assert_failure
  assert_output --partial 'ещё нет установщика перехода с v0'
  [ ! -e "$UPGRADE_TRACE" ]
  [ "$(git -C "$PLATFORM_FIXTURE" rev-parse HEAD)" = "$OLD_REV" ]
}
