#!/usr/bin/env bash
# tooling/wire-docs.sh — раздаёт ПЛАТФОРМЕННЫЕ доки в проект.
#
# Та же болезнь, что была со скиллами, только уровнем выше: AGENTS.md и
# MONOREPO.md копировались в проект при `adc new` и дальше жили своей
# жизнью — платформа не могла поправить общее правило нигде, кроме новых
# проектов. Теперь общее раздаётся отсюда, а проект ДОПОЛНЯЕТ его своим файлом.
#
# Раздаётся симлинком на монтируемый клон платформы: проза, править её в проекте
# нельзя (маунт read-only), правка в платформе видна сразу.
#
# ПЕРЕКРЫТИЕ: если в проекте по этому пути лежит НАСТОЯЩИЙ файл (не наш симлинк) —
# он выигрывает, мы не трогаем. Так проект может забрать док себе, а мы не
# затираем то, чего не писали.
#
# Зовётся из wire-agent-skills.sh, отдельно дёргать не нужно.

set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$PWD}"
TOOLING_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PLATFORM_ROOT="$(dirname "$TOOLING_DIR")"
DOCS_SRC="${AI_DEVCONTAINER_DOCS:-$PLATFORM_ROOT/docs}"

C_GREEN='\033[0;32m'; C_YELLOW='\033[0;33m'; C_DIM='\033[2m'; C_RESET='\033[0m'
log()  { echo -e "${C_GREEN}==>${C_RESET} $*"; }
warn() { echo -e "${C_YELLOW}!! ${C_RESET}$*" >&2; }
dim()  { echo -e "${C_DIM}$*${C_RESET}"; }

[ -d "$DOCS_SRC" ] || { dim "  нет $DOCS_SRC — доки не раздаю"; exit 0; }

# Репозиторий платформы, открытый сам в себе: раздавать себе же нечего.
if [ "$(readlink -f "$REPO_ROOT")" = "$(readlink -f "$PLATFORM_ROOT")" ]; then
  dim "  это репозиторий платформы — доки не раздаю"
  exit 0
fi

# исходник в платформе → путь в проекте
DOCS="
AGENTS.platform.md:AGENTS.platform.md
MONOREPO.md:MONOREPO.md
skills-README.md:.agents/skills/README.md
plans-README.md:plans/README.md
mcp-secrets.env.example:.agents/mcp.secrets.env.example
"

linked=0 kept=0
while IFS=: read -r src dst; do
  [ -n "${src:-}" ] || continue
  from="$DOCS_SRC/$src"
  to="$REPO_ROOT/$dst"
  [ -f "$from" ] || { warn "нет $from — пропускаю"; continue; }

  if [ -e "$to" ] && [ ! -L "$to" ]; then
    kept=$((kept + 1))
    dim "  $dst — файл проекта, не трогаю (платформенный вариант: $from)"
    continue
  fi
  mkdir -p "$(dirname "$to")"
  ln -sfn "$from" "$to"
  linked=$((linked + 1))
done <<EOF
$DOCS
EOF

log "Доки платформы: $linked разложено$([ "$kept" -gt 0 ] && echo ", $kept оставлено за проектом")"
