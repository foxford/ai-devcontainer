#!/usr/bin/env bash
set -euo pipefail

CYAN='\033[0;36m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; DIM='\033[2m'; RESET='\033[0m'
log()  { echo -e "${CYAN}→${RESET} $1"; }
ok()   { echo -e "${GREEN}✓${RESET} $1"; }
warn() { echo -e "${YELLOW}!${RESET} $1"; }

usage() {
  echo -e "Usage: ${CYAN}graphify-view${RESET} [port]"
  echo ""
  echo "  Поднимает локальный http-сервер для graphify-out/ и печатает ссылки"
  echo "  на интерактивный граф. Если graph.html ещё не построен, строит его."
  echo ""
  echo -e "  ${DIM}graphify-view${RESET}        # порт 4242 (проброшен в devcontainer)"
  echo -e "  ${DIM}graphify-view 3001${RESET}   # другой проброшенный порт"
  exit 0
}

[[ "${1:-}" == "-h" || "${1:-}" == "--help" ]] && usage

PORT="${1:-4242}"

# Порт уже занят? Понятная ошибка вместо python-трейсбека (Address already in use).
if (exec 3<>"/dev/tcp/127.0.0.1/${PORT}") 2>/dev/null; then
  exec 3>&- 3<&-
  warn "Порт ${PORT} уже занят — возможно, graphify-view уже запущен в другом терминале."
  warn "Останови тот сервер (Ctrl+C) или укажи другой порт:  graphify-view <port>"
  exit 1
fi

# Корень проекта: git-root, иначе текущая директория
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
OUT="$ROOT/graphify-out"

# Если штатного graph.html нет, строим граф (headless AST, без LLM)
if [ ! -f "$OUT/graph.html" ]; then
  if ! command -v graphify >/dev/null 2>&1; then
    warn "graphify не установлен и graph.html отсутствует."
    warn "Запусти сетап: bash scripts/post-create-setup.sh"
    exit 1
  fi
  warn "graph.html не найден, строю граф (graphify update .)..."
  ( cd "$ROOT" && GRAPHIFY_VIZ_NODE_LIMIT="${GRAPHIFY_VIZ_NODE_LIMIT:-50000}" graphify update . )
fi

[ -f "$OUT/graph.html" ] || { warn "graph.html так и не появился, см. вывод выше."; exit 1; }

# Осмысленные имена сообществ по путям вместо "Community N" (тихо, best-effort —
# на случай если graph.html собран обычным `graphify update .`).
command -v graphify-label-paths >/dev/null 2>&1 && graphify-label-paths >/dev/null 2>&1 || true

ok "Граф: $OUT"
log "Открой в браузере:"
echo -e "    ${GREEN}http://localhost:${PORT}/graph.html${RESET}       ${DIM}интерактивный граф${RESET}"
[ -f "$OUT/GRAPH_TREE.html" ] && \
  echo -e "    ${GREEN}http://localhost:${PORT}/GRAPH_TREE.html${RESET}  ${DIM}дерево (collapsible)${RESET}"
echo -e "    ${DIM}GRAPH_REPORT.md: текстовая сводка (открой в IDE)${RESET}"
echo -e "    ${DIM}Ctrl+C останавливает сервер${RESET}"
echo ""

cd "$OUT"
# --protocol HTTP/1.1: дефолтный HTTP/1.0 (connection-close) режется проброс-туннелем
# VS Code на больших ответах (graph.html ~6 МБ) → ERR_CONTENT_LENGTH_MISMATCH.
exec python3 -m http.server "$PORT" --bind 0.0.0.0 --protocol HTTP/1.1
