# Хелперы для PATH-моков внешних CLI (docker, git, codex, yq, dsh, npm, ...).
# load'ится из .bats-файлов через: load '../helpers/mocks'

# Заводит каталог для моков и кладёт его в начало PATH. Звать из setup().
mocks_init() {
  MOCK_BIN_DIR="$(mktemp -d)"
  MOCK_CALLS_DIR="$(mktemp -d)"
  PATH="$MOCK_BIN_DIR:$PATH"
}

mocks_cleanup() {
  [ -n "${MOCK_BIN_DIR:-}" ] && rm -rf "$MOCK_BIN_DIR"
  [ -n "${MOCK_CALLS_DIR:-}" ] && rm -rf "$MOCK_CALLS_DIR"
}

# mock_bin <имя> [exit_code] [stdout]
# Заводит исполняемый файл-заглушку $MOCK_BIN_DIR/<имя>. Каждый вызов
# дописывает полученные аргументы (по одной строке на аргумент, разделённые
# пустой строкой между вызовами) в $MOCK_CALLS_DIR/<имя>.log — для проверки
# через mock_calls/mock_call_count. Возвращает exit_code (по умолчанию 0),
# печатает stdout (по умолчанию пусто).
mock_bin() {
  local name="$1" exit_code="${2:-0}" stdout="${3:-}"
  local log="$MOCK_CALLS_DIR/$name.log"
  cat > "$MOCK_BIN_DIR/$name" <<EOF
#!/usr/bin/env bash
{
  for a in "\$@"; do printf '%s\n' "\$a"; done
  printf '%s\n' '---'
} >> "$log"
$([ -n "$stdout" ] && printf 'printf %%s\\\\n %q\n' "$stdout")
exit $exit_code
EOF
  chmod +x "$MOCK_BIN_DIR/$name"
}

# Число вызовов мока <имя>.
mock_call_count() {
  local name="$1" log="$MOCK_CALLS_DIR/$name.log"
  [ -f "$log" ] || { echo 0; return 0; }
  grep -c '^---$' "$log"
}

# Все аргументы всех вызовов мока <имя>, по одному на строку (для grep/assert).
mock_calls() {
  local name="$1" log="$MOCK_CALLS_DIR/$name.log"
  [ -f "$log" ] && cat "$log" || true
}
