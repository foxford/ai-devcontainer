#!/usr/bin/env bash
# tests/run.sh [путь...] — гоняет bats-тесты платформы.
# Без аргументов — весь tests/ (кроме tests/bats/lib/, это сами фреймворки).
set -euo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BATS="$TESTS_DIR/bats/lib/bats-core/bin/bats"

if [ ! -x "$BATS" ]; then
  echo "bats-core не найден в $BATS — выполни: git submodule update --init --recursive" >&2
  exit 1
fi

if [ "$#" -gt 0 ]; then
  exec "$BATS" --recursive "$@"
fi

# Только наши тестовые каталоги — не весь tests/ (там же живут submodule'ы
# bats-core/bats-support/bats-assert со СВОИМИ .bats-тестами фреймворка).
shopt -s nullglob
suites=("$TESTS_DIR"/tooling_*/ "$TESTS_DIR"/bin_*/)
shopt -u nullglob

if [ "${#suites[@]}" -eq 0 ]; then
  echo "Тестов пока нет (tests/tooling_*/, tests/bin_*/) — ok"
  exit 0
fi

exec "$BATS" --recursive "${suites[@]}"
