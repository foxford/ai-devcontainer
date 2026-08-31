#!/usr/bin/env bash
# post-create девконтейнера САМОЙ платформы.
# Отличия от tooling/post-create-setup.sh (который для проектов из skeleton):
#   • монорепа живёт в skeleton/ — install/линты гоняются там;
#   • seed не нужен (skeleton и ЕСТЬ источник);
#   • скиллы: платформенный слой — это наш же skills/, проектный слой пустой
#     (skeleton/.agents/skills через симлинк .agents) — правя skills/, ты сразу
#     видишь результат теми же агентами, что и проекты.

set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

C_GREEN='\033[0;32m'; C_RESET='\033[0m'
log() { echo -e "${C_GREEN}==>${C_RESET} $*"; }

export COREPACK_ENABLE_DOWNLOAD_PROMPT=0 UV_NO_PROGRESS=1 NPM_CONFIG_PROGRESS=false CI=${CI:-1}

log "[1/5] pnpm install (skeleton — рабочая монорепа)"
( cd skeleton && pnpm install --reporter=append-only )

log "[2/5] CLI + хелперы + obsidian → ~/.local/bin"
mkdir -p "$HOME/.local/bin"
# та же команда, что на хосте; здесь корень платформы — рабочая копия
ln -sfn "$PWD/bin/adc" "$HOME/.local/bin/adc"
for helper in tooling/helpers/*.sh; do
  install -m 0755 "$helper" "$HOME/.local/bin/$(basename "$helper" .sh)"
done
install -m 0755 skeleton/.devcontainer/obsidian "$HOME/.local/bin/obsidian"

# Страховка переписки Claude (retention + первый снапшот) — см. helpers/claude-snapshot.sh
"$HOME/.local/bin/claude-snapshot" retention || true
"$HOME/.local/bin/claude-snapshot" snapshot || true

log "[3/5] AI tools"
# Вывод — в файл, не в наш stdout: инсталлеры (npm/uv/hermes) форкают фоновые
# процессы, которые наследуют дескрипторы. Если они унаследуют НАШ пайп,
# postCreate «висит» после завершения скрипта и VS Code пишет interrupted.
AI_LOG="/tmp/install-ai-tools.log"
if bash tooling/install-ai-tools.sh </dev/null >"$AI_LOG" 2>&1; then
  tail -20 "$AI_LOG"
else
  cat "$AI_LOG"; echo "!! install-ai-tools.sh failed (лог: $AI_LOG)" >&2; exit 1
fi

log "[4/5] Скиллы → агенты (слои: skills/ + skeleton/.agents/skills)"
[ -e .agents ] || ln -s skeleton/.agents .agents
bash tooling/wire-agent-skills.sh || true

log "[5/5] Браузер Playwright для e2e скелета"
# Скелет несёт настоящий e2e-раннер, и правки в нём проверяются прогоном
# (см. AGENTS.md). Браузер лежит в named volume, так что качается один раз
# на все rebuild'ы. Не блокирует: без сети платформа обязана подниматься.
if [ -x skeleton/node_modules/.bin/playwright ]; then
  ( cd skeleton && pnpm exec playwright install chromium ) \
    || echo "!! браузер не поставился — e2e скелета не прогнать (пробуй: cd skeleton && pnpm run e2e:install)" >&2
else
  echo "   playwright в skeleton не установлен — пропускаю"
fi

# MCP платформе не раздаём — см. шапку tooling/wire-mcp.sh (проектный слой
# .agents здесь симлинк в skeleton, и перекрытие уехало бы во ВСЕ новые проекты).

log "Готово. Проверки платформы: cd skeleton && pnpm run lint / type-check / test / build / e2e"
