#!/usr/bin/env bash
# tooling/plans.sh — реализация `ai-devcontainer plans`: обзор задач проекта.
# Напрямую не зовут: точка входа одна и та же на хосте и в контейнере.
#
# Зачем команда вообще. Раньше «что в работе» читалось прямо из дерева: закрытая
# задача уезжала в plans/archive/<год>/, и в plans/ оставались одни активные.
# Цена оказалась несоразмерной — путь кодировал статус, который и так лежит во
# фронтматтере, а каждое закрытие ломало ссылки снаружи (спека, соседние задачи,
# тексты коммитов; последние не чинятся вообще). Пути сделали неизменными, и
# разделение активного и закрытого переехало сюда.
#
# Запрос при этом умеет больше, чем дерево: незакрытые сортируются по `updated`,
# так что сверху оказывается то, что дольше всех не двигалось. Это и есть
# вопрос, который человек на самом деле задаёт каталогу планов, — дерево на него
# не отвечало никогда.
#
# Раскладка и файлы прошлой схемы (плоско в plans/, archive/<год>/, epic.md
# вместо task.md) не игнорируются, а помечаются: проект с ними ещё не переехал,
# и увидеть это надо здесь, а не обнаружить через полгода по битой ссылке.
# Порядок разового перевода — в plans/README.md.
#
# Без зависимостей: coreutils + awk. Даты — строки YYYY-MM-DD и сравниваются
# лексикографически, поэтому арифметика по датам (и GNU-only `date -d`) не нужна.

set -euo pipefail

# Терминал может быть открыт в подкаталоге — корень ищем вверх, как в CLI.
REPO_ROOT="${REPO_ROOT:-$PWD}"
while [ "$REPO_ROOT" != "/" ] && [ ! -d "$REPO_ROOT/.git" ] && [ ! -d "$REPO_ROOT/plans" ]; do
  REPO_ROOT="$(dirname "$REPO_ROOT")"
done
[ "$REPO_ROOT" = "/" ] && REPO_ROOT="$PWD"

PLANS="$REPO_ROOT/plans"

C_GREEN='\033[0;32m'; C_YELLOW='\033[0;33m'; C_RED='\033[0;31m'; C_DIM='\033[2m'; C_RESET='\033[0m'
log()  { echo -e "${C_GREEN}==>${C_RESET} $*"; }
warn() { echo -e "${C_YELLOW}!! ${C_RESET}$*" >&2; }
err()  { echo -e "${C_RED}xx ${C_RESET}$*" >&2; }
dim()  { echo -e "${C_DIM}$*${C_RESET}"; }

SHOW_ALL=0
case "${1:-}" in
  --all|-a) SHOW_ALL=1 ;;
  "")       ;;
  *)        err "Использование: ai-devcontainer plans [--all]"; exit 1 ;;
esac

if [ ! -d "$PLANS" ]; then
  dim "нет $PLANS — каталог планов в этом проекте ещё не заводили"
  dim "формат и правила: docs/plans-README.md в репозитории платформы"
  exit 0
fi

# Значение поля из YAML-фронтматтера (первый блок ---), как в wire-agent-skills.sh.
field() {
  awk -v field="$2" '
    NR==1 && $0=="---" { inb=1; next }
    inb && $0=="---"   { exit }
    inb && $0 ~ "^"field":" {
      sub("^"field":[[:space:]]*", "")
      gsub(/^"|"$/, "")
      print
      exit
    }
  ' "$1"
}

# Один каталог — одна задача, даже если в нём лежат оба файла: task.md главнее,
# epic.md тогда просто недоубранный остаток. Сортировка find'а даёт epic.md
# раньше task.md, поэтому второй затирает первого сам.
declare -A FILE_BY_DIR=()
while IFS= read -r f; do
  d="$(dirname "$f")"
  if [ "$(basename "$f")" = "task.md" ] || [ -z "${FILE_BY_DIR[$d]:-}" ]; then
    FILE_BY_DIR["$d"]="$f"
  fi
done < <(find "$PLANS" -type f \( -name task.md -o -name epic.md \) | sort)

OPEN=""; CLOSED=""; LEGACY=0; TOTAL=0

for dir in $(printf '%s\n' "${!FILE_BY_DIR[@]}" | sort); do
  file="${FILE_BY_DIR[$dir]}"
  rel="${dir#"$PLANS"/}"
  TOTAL=$((TOTAL + 1))

  st="$(field "$file" status)";  st="${st:-—}"
  up="$(field "$file" updated)"
  [ -n "$up" ] || up="$(field "$file" created)"
  # Без даты задача всплывает наверх списка незакрытых: «неизвестно когда
  # трогали» — это тоже сигнал, и прятать его в хвост неправильно.
  up="${up:-0000-00-00}"

  # Отметки прошлой схемы. plans/<год>/<задача>/task.md — целевая раскладка,
  # всё остальное подлежит разовому переводу (см. plans/README.md).
  mark=""
  case "$rel" in
    archive/*)              mark="  ← archive/" ;;
    [0-9][0-9][0-9][0-9]/*) ;;
    *)                      mark="  ← вне plans/<год>/" ;;
  esac
  if [ "$(basename "$file")" = "epic.md" ]; then
    if [ -n "$mark" ]; then mark="$mark, epic.md"; else mark="  ← epic.md"; fi
  fi
  [ -n "$mark" ] && LEGACY=$((LEGACY + 1))

  line="$(printf '  %-9s %s  %s%s' "$st" "$up" "$rel" "$mark")"
  case "$st" in
    done|dropped) CLOSED="$CLOSED$up|$line"$'\n' ;;
    *)            OPEN="$OPEN$up|$line"$'\n' ;;
  esac
done

if [ "$TOTAL" = 0 ]; then
  dim "в $PLANS нет ни одной задачи (task.md)"
  dim "формат и правила: plans/README.md"
  exit 0
fi

n_open="$(printf '%s' "$OPEN" | grep -c . || true)"
n_closed="$(printf '%s' "$CLOSED" | grep -c . || true)"

log "plans: задач $TOTAL — в работе $n_open, закрыто $n_closed"
echo

# Сортировка по дате (ключ до |), затем ключ снимаем.
[ "$n_open" -gt 0 ] && printf '%s' "$OPEN" | sort | cut -d'|' -f2-

if [ "$SHOW_ALL" = 1 ]; then
  [ "$n_closed" -gt 0 ] && { echo; printf '%s' "$CLOSED" | sort | cut -d'|' -f2-; }
elif [ "$n_closed" -gt 0 ]; then
  echo
  dim "  закрытых: $n_closed (показать: ai-devcontainer plans --all)"
fi

if [ "$LEGACY" -gt 0 ]; then
  echo
  warn "на старой схеме: $LEGACY из $TOTAL (отметки справа)"
  echo "    Порядок разового перевода — «Переезд со старой схемы» в plans/README.md" >&2
fi
