#!/usr/bin/env bash
# post-create-setup.sh — проектный сетап, запускается из postCreateCommand.
#
# ВАЖНО про пути: этот скрипт живёт НЕ в проекте, а запечён в образ
# (/opt/dev-tooling). Поэтому разведены два корня:
#   TOOLING_DIR  — где лежат хелперы и seed-шаблоны (рядом с этим файлом)
#   PROJECT_ROOT — репозиторий, который сейчас открыт ($PWD при postCreate)
#
# Шаги:
#   1. seed .hermes                  ─ дефолт из платформы, если его ещё нет в репо
#   2. pnpm install                  ─ зависимости монорепы
#   3. helpers → ~/.local/bin        ─ pnpm-patch-dep и т.д.
#   4. install-ai-tools.sh           ─ claude, opencode, codex, hermes, dsh, graphify
#   5. graphify update .             ─ граф проекта (локально в graphify-out/)
#   6. adc sync             ─ скиллы, доки и MCP платформы → проект
#   7. playwright install            ─ браузеры в volume (раннер проекта и/или MCP)
#   8. .hermes/bootstrap.sh          ─ профайлы, hooks
#
# Идемпотентен: можно дёргать руками сколько угодно —
#   bash /opt/dev-tooling/post-create-setup.sh

set -euo pipefail

TOOLING_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="${PROJECT_ROOT:-$PWD}"

C_GREEN='\033[0;32m'; C_YELLOW='\033[0;33m'; C_RESET='\033[0m'
log()  { echo -e "${C_GREEN}==>${C_RESET} $*"; }
warn() { echo -e "${C_YELLOW}!! ${C_RESET}$*" >&2; }

cd "$PROJECT_ROOT"

# postCreate идёт без TTY: прогресс-бары (uv/npm/pnpm) засирают лог тысячами
# строк с \r и мешают читать реальные ошибки. Глушим их.
export COREPACK_ENABLE_DOWNLOAD_PROMPT=0
export UV_NO_PROGRESS=1
export NPM_CONFIG_PROGRESS=false
export CI=${CI:-1}

log "проект: $PROJECT_ROOT | тулинг: $TOOLING_DIR"

# ── 1. Seed дефолтов из платформы ──────────────────────────────
# Источник правды — skeleton/ в клоне платформы (примонтирован read-only
# рядом с tooling/). Проекты из skeleton несут .hermes и скиллы с рождения;
# seed нужен только репозиториям, созданным НЕ из skeleton. Дальше проект
# владеет копией сам (правит и коммитит).
PLATFORM_DIR="$(dirname "$TOOLING_DIR")"
seed_dir() {
  local src="$1" dst="$2"
  [ -d "$src" ] || return 0
  # только .gitkeep внутри — шаблон пустой, сидить нечего
  [ -n "$(find "$src" -type f ! -name '.gitkeep' -print -quit)" ] || return 0
  if [ -e "$dst" ]; then return 0; fi
  cp -a "$src" "$dst"
  log "  seed: $(basename "$dst") ← шаблон платформы"
}
log "[1/8] Seed .hermes (если отсутствует)"
seed_dir "$PLATFORM_DIR/skeleton/.hermes" "$PROJECT_ROOT/.hermes"
# Скиллы НЕ сидим: с переходом на overlay стоковые живут в платформе
# ($PLATFORM_DIR/skills) и подмешиваются на шаге 6. В проекте лежит только то,
# чем он отличается, — форки и свои скиллы.
mkdir -p "$PROJECT_ROOT/.agents/skills"

# ── 2. pnpm install ────────────────────────────────────────────
# Нода и pnpm уже запечены в образ (base + setup.sh на этапе build),
# поэтому здесь никакой возни с asdf/corepack не нужно.
log "[2/8] pnpm install"
pnpm install --reporter=append-only

# ── 3. Helper scripts → ~/.local/bin ───────────────────────────
log "[3/8] Хелперы → ~/.local/bin"
BIN_DIR="$HOME/.local/bin"
mkdir -p "$BIN_DIR"
# CLI платформы — той же командой, что и на хосте. Внутри контейнера из него
# работают project-скоупные вещи (sync, skill, doctor, update→sync); new и
# ensure-image упрутся в read-only маунт и скажут об этом явно.
if [ -f "$PLATFORM_DIR/bin/adc" ]; then
  ln -sfn "$PLATFORM_DIR/bin/adc" "$BIN_DIR/adc"
  log "  → adc (CLI платформы)"
fi

if [ -d "$TOOLING_DIR/helpers" ]; then
  for helper in "$TOOLING_DIR"/helpers/*.sh; do
    [ -e "$helper" ] || continue
    name="$(basename "$helper" .sh)"
    install -m 0755 "$helper" "$BIN_DIR/$name"
    log "  → $name"
  done
fi

# Переписка Claude: снять автоуборку транскриптов и сделать первый снапшот.
# Дефолтный retention (30 дней) молча удаляет .jsonl-диалоги, а ~/.claude.json
# умеет обнуляться вместе со списком сессий — так в одном из проектов пропала
# вся история. Дальше снапшоты обновляются раз в сутки из motd.
if [ -x "$BIN_DIR/claude-snapshot" ]; then
  "$BIN_DIR/claude-snapshot" retention || true
  "$BIN_DIR/claude-snapshot" snapshot || true
fi

# ── 4. AI tools ────────────────────────────────────────────────
log "[4/8] AI tools (claude / opencode / codex / hermes / dsh / graphify)"
# Вывод — в файл: инсталлеры форкают фоновые процессы, наследующие дескрипторы.
# Унаследуют наш stdout-пайп → postCreate «висит» после завершения скрипта и
# VS Code пишет interrupted, хотя всё прошло. Файл этот пайп не держит.
AI_LOG="/tmp/install-ai-tools.log"
if bash "$TOOLING_DIR/install-ai-tools.sh" </dev/null >"$AI_LOG" 2>&1; then
  tail -20 "$AI_LOG"
else
  cat "$AI_LOG"; warn "install-ai-tools.sh failed (полный лог: $AI_LOG)"; exit 1
fi

# ── 5. Graphify project graph ──────────────────────────────────
log "[5/8] Graphify project graph"
export GRAPHIFY_VIZ_NODE_LIMIT="${GRAPHIFY_VIZ_NODE_LIMIT:-50000}"
if ! command -v graphify >/dev/null 2>&1; then
  warn "graphify не найден в PATH, пропускаю билд графа"
elif [ -f graphify-out/graph.html ]; then
  log "  graphify-out/graph.html уже есть, пропускаю (обновляй сам: graphify update .)"
else
  if graphify update .; then
    bash "$TOOLING_DIR/helpers/graphify-label-paths.sh" || true
    log "  Готово: graphify-out/. Смотреть: graphify-view"
  else
    warn "  graphify update завершился с ошибкой (агенты будут работать без графа)"
  fi
fi

# ── 6. Применить платформу к проекту ───────────────────────────
# Ровно та же команда, что человек набирает руками, — не отдельный набор
# вызовов: два пути «разложить платформу по проекту» разъезжаются, и однажды
# постCreate начинает раскладывать не то же самое, что sync. Заодно команда
# ставит отметку применённой ревизии, по которой motd потом видит, что
# платформа уехала вперёд.
# Зовём по абсолютному пути: ~/.local/bin в PATH этого шелла может не быть.
log "[6/8] Платформа → проект: скиллы, доки, MCP (adc sync)"
bash "$PLATFORM_DIR/bin/adc" sync || warn "adc sync failed (не блокирует)"

# ── 7. Браузеры Playwright ─────────────────────────────────────
# Ставим В VOLUME ($PLAYWRIGHT_BROWSERS_PATH, задан в образе), а не в образ:
# ревизия браузера привязана к версии пакета playwright, и общий для всех
# проектов образ неизбежно разъехался бы с их package.json.
#
# Ставим ДО двух раз, и это не ошибка: у раннера проекта (@playwright/test) и
# у браузерного MCP (@playwright/mcp тянет свой playwright) версии разные, а
# значит разные ревизии chromium. Обе живут в volume рядом, каждая под своей.
#
# Ни один вызов не блокирует postCreate: без сети или под корпоративным
# прокси проект обязан подниматься дальше, просто без браузера.
log "[7/8] Браузеры Playwright (chromium → ${PLAYWRIGHT_BROWSERS_PATH:-~/.cache/ms-playwright})"
if [ -n "${AI_DEVCONTAINER_SKIP_BROWSERS:-}" ]; then
  log "  AI_DEVCONTAINER_SKIP_BROWSERS выставлен — пропускаю"
else
  # 7a. Раннер проекта — только если проект вообще завёл playwright у себя.
  if ls playwright.config.* >/dev/null 2>&1 && [ -x node_modules/.bin/playwright ]; then
    log "  раннер проекта: pnpm exec playwright install chromium"
    pnpm exec playwright install chromium || warn "  не поставился браузер раннера (поставь потом: pnpm exec playwright install chromium)"
  else
    log "  раннера playwright в проекте нет — пропускаю"
  fi

  # 7b. Браузерный MCP. Версию берём из РАЗДАННОГО конфига, а не из константы:
  # так пин остаётся ровно в одном месте (mcp/servers.json + перекрытие проекта).
  MCP_PIN=""
  if [ -f "$PROJECT_ROOT/.mcp.json" ] && command -v jq >/dev/null 2>&1; then
    MCP_PIN="$(jq -r '.mcpServers.playwright.args[]? | select(startswith("@playwright/mcp@"))' \
      "$PROJECT_ROOT/.mcp.json" 2>/dev/null | head -1)"
  fi
  if [ -n "$MCP_PIN" ]; then
    # `npx --package=X playwright …` запускает playwright ИЗ зависимостей X,
    # то есть ровно той версии, что нужна этому MCP-серверу.
    log "  браузерный MCP: npx --package=$MCP_PIN playwright install chromium"
    npx -y --package="$MCP_PIN" playwright install chromium \
      || warn "  не поставился браузер для MCP (поставь потом: npx -y --package=$MCP_PIN playwright install chromium)"
  else
    log "  браузерный MCP не раздан — пропускаю"
  fi

  # 8c. Стабильный симлинк на chromium для chrome-devtools-mcp.
  # У того нет своего браузера и он умеет только --executablePath, а реальный
  # путь ревизионный (chromium-1237/chrome-linux64/chrome) и меняется с каждым
  # обновлением пакета. Пин в mcp/servers.json указывает на этот симлинк;
  # пока его нет, сервер не раздаётся (x-requires: path:).
  BROWSERS_DIR="${PLAYWRIGHT_BROWSERS_PATH:-$HOME/.cache/ms-playwright}"
  CHROMIUM_BIN="$(find "$BROWSERS_DIR" -path '*/chrome-linux64/chrome' -type f 2>/dev/null | sort -V | tail -1)"
  if [ -n "$CHROMIUM_BIN" ]; then
    ln -sfn "$CHROMIUM_BIN" "$BROWSERS_DIR/chrome-current"
    log "  chrome-current → $CHROMIUM_BIN"
    # Симлинк появился ПОСЛЕ шага 7, а тот гейтит chrome-devtools по его
    # наличию — значит на первом postCreate сервер прошёл бы мимо и появился
    # только со следующего запуска. Догоняем: wire-mcp идемпотентен и дёшев.
    if [ -f "$PROJECT_ROOT/.mcp.json" ] && command -v jq >/dev/null 2>&1 \
       && ! jq -e '.mcpServers["chrome-devtools"]' "$PROJECT_ROOT/.mcp.json" >/dev/null 2>&1; then
      log "  chrome-devtools стал доступен — перераздаю MCP"
      bash "$TOOLING_DIR/wire-mcp.sh" || warn "wire-mcp.sh (повтор) failed (не блокирует)"
    fi
  else
    log "  полного chromium нет — chrome-devtools-mcp раздан не будет"
  fi
fi

# ── 8. Hermes per-project bootstrap ────────────────────────────
log "[8/8] Hermes bootstrap"
if [ ! -d ".hermes" ]; then
  warn "нет .hermes/ — пропускаю"
elif ! command -v hermes >/dev/null 2>&1; then
  warn "hermes CLI не в PATH — пропускаю. Открой новый шелл и запусти: bash .hermes/bootstrap.sh"
elif ! bash .hermes/bootstrap.sh; then
  warn ".hermes/bootstrap.sh упал. Если это первый запуск — нужна авторизация:"
  warn "  hermes setup && bash .hermes/bootstrap.sh"
fi

log "Готово."
