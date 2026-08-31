#!/usr/bin/env bash
set -euo pipefail

export AI_TOOLS_HOME="${AI_TOOLS_HOME:-/opt/ai-tools}"
mkdir -p "$AI_TOOLS_HOME/bin"
export PATH="$AI_TOOLS_HOME/bin:$PATH"

log() { echo "==> $*"; }

# Claude
# ВАЖНО: проверяем не «бинарь есть», а «реально работает». /opt/ai-tools —
# общий volume: в нём переживает rebuild claude-ЛАУНЧЕР, но сам бинарь лежит
# в ~/.local/share/claude/versions/ (контейнер-локально) и после rebuild
# исчезает → лаунчер падает с "exec: : Permission denied". Тогда ставим заново.
claude_works() {
  command -v claude >/dev/null 2>&1 && claude --version >/dev/null 2>&1
}

if ! claude_works; then
  if command -v claude >/dev/null 2>&1; then
    log "Found broken claude launcher (versions dir lost after rebuild) — reinstalling..."
    rm -f "$AI_TOOLS_HOME/bin/claude" "$HOME/.local/bin/claude"
  else
    log "Installing claude..."
  fi
  curl -fsSL https://claude.ai/install.sh | bash -s
  if [ -f "$HOME/.local/bin/claude" ] && [ ! -f "$AI_TOOLS_HOME/bin/claude" ]; then
    mv "$HOME/.local/bin/claude" "$AI_TOOLS_HOME/bin/claude"
  fi
  command -v claude >/dev/null && claude --version || true
else
  log "claude already installed"
fi

# Opencode — конфиг в ~/.config/opencode (bind на хост), бинарь в /opt/ai-tools
if ! command -v opencode >/dev/null 2>&1; then
  log "Installing opencode..."
  curl -fsSL https://opencode.ai/install | bash -s
  if [ -f "$HOME/.opencode/bin/opencode" ] && [ ! -f "$AI_TOOLS_HOME/bin/opencode" ]; then
    mv "$HOME/.opencode/bin/opencode" "$AI_TOOLS_HOME/bin/opencode"
  fi
  command -v opencode >/dev/null && opencode --version || true
else
  log "opencode already installed"
fi

# Codex — npm global в /opt/ai-tools
if ! command -v codex >/dev/null 2>&1; then
  log "Installing codex..."
  NPM_CONFIG_PREFIX="$AI_TOOLS_HOME" npm i -g @openai/codex
  command -v codex >/dev/null && codex --version || true
else
  log "codex already installed"
fi

# DSH — DeepSeek Harness, агентный рантайм DeepSeek (npm global в /opt/ai-tools,
# как codex). Требует Node ^22.19 || >=24 — в образе 26.x, условие выполнено.
#
# Установка НЕ блокирующая, и это осознанно: dsh в developer preview, апстрим
# обещает ломающие изменения, а сборка контейнера не должна падать из-за
# агента, которым проект может и не пользоваться. Остальные три агента при
# этом встают как раньше.
#
# Профили dsh (~/.dsh/profiles/<имя>) он доставляет себе сам при первом старте:
# это pnpm-зависимости бандлов, им место в пер-проектном маунте ~/.dsh, а не в
# образе. Ключ — DEEPSEEK_API_KEY в окружении либо ~/.dsh/.env.
#
# --allow-scripts: с npm 11.5 lifecycle-скрипты зависимостей при global install
# блокируются по умолчанию. У dsh их несколько, и значимый ровно один —
# `ensure-spawn-helper` у dsh-subprocess-local: он возвращает бит +x
# помощнику node-pty, без которого не поднимается терминал агента. Сейчас на
# linux-x64 этот помощник в prebuild'ах и не лежит (он маковый), то есть
# скрипт вхолостую, — но привязываться к этому не хочется: появится он в
# следующей версии node-pty, и терминал отвалится молча. node-pty и koffi в
# списке за компанию, у них там сборка нативной части.
# Флаг молодой: если npm его не знает, ставим как раньше — блокировка скриптов
# сама по себе установку не ломает, она только предупреждает.
DSH_SCRIPTS="@deepseek-ai/dsh-subprocess-local,node-pty,koffi"
if ! command -v dsh >/dev/null 2>&1; then
  log "Installing dsh (DeepSeek Harness)..."
  if NPM_CONFIG_PREFIX="$AI_TOOLS_HOME" npm i -g --allow-scripts="$DSH_SCRIPTS" @deepseek-ai/dsh \
     || NPM_CONFIG_PREFIX="$AI_TOOLS_HOME" npm i -g @deepseek-ai/dsh; then
    command -v dsh >/dev/null && dsh --version || true
  else
    log "  ! dsh не поставился — не блокирует (поставить руками: NPM_CONFIG_PREFIX=$AI_TOOLS_HOME npm i -g @deepseek-ai/dsh)"
  fi
else
  log "dsh already installed: $(dsh --version 2>/dev/null || echo '?')"
fi

# Hermes
# Проверка не "бинарь существует", а "реально работает". На clean rebuild
# wrapper /opt/ai-tools/bin/hermes сохраняется в named volume, но его venv
# в ~/.hermes/hermes-agent/ может быть сломан (например, потерял python).
# В таком случае переустанавливаем.
hermes_works() {
  command -v hermes >/dev/null 2>&1 && hermes --version >/dev/null 2>&1
}

if ! hermes_works; then
  if command -v hermes >/dev/null 2>&1; then
    log "Found broken hermes — cleaning venv and reinstalling..."
    rm -rf "$HOME/.hermes/hermes-agent"
  else
    log "Installing hermes..."
  fi
  curl -fsSL https://raw.githubusercontent.com/NousResearch/hermes-agent/main/scripts/install.sh | bash
  if [ -f "$HOME/.local/bin/hermes" ] && [ ! -f "$AI_TOOLS_HOME/bin/hermes" ]; then
    mv "$HOME/.local/bin/hermes" "$AI_TOOLS_HOME/bin/hermes"
  fi
  hermes_works && log "hermes installed: $(hermes --version)" || log "hermes still not working — see output above"
else
  log "hermes already installed: $(hermes --version)"
fi

# uv — менеджер Python-пакетов (нужен для graphify; ставит изолированные tool-venv)
if [ ! -x "$AI_TOOLS_HOME/bin/uv" ]; then
  log "Installing uv..."
  export UV_INSTALL_DIR="$AI_TOOLS_HOME/bin"
  export INSTALLER_NO_MODIFY_PATH=1
  curl -LsSf https://astral.sh/uv/install.sh | sh
  command -v uv >/dev/null && uv --version || true
else
  log "uv already installed"
fi

# Graphify — knowledge graph CLI (пакет graphifyy).
# Tool-venv лежит в named volume /opt/ai-tools и использует системный python3.
# На clean rebuild venv может потерять интерпретатор (сменилась версия python в
# образе) — в этом случае переустанавливаем, по аналогии с hermes.
export UV_TOOL_BIN_DIR="$AI_TOOLS_HOME/bin"
export UV_TOOL_DIR="$AI_TOOLS_HOME/uv-tools"

graphify_works() {
  command -v graphify >/dev/null 2>&1 && graphify --version >/dev/null 2>&1
}

if ! graphify_works; then
  if command -v graphify >/dev/null 2>&1; then
    log "Found broken graphify — reinstalling..."
    uv tool uninstall graphifyy >/dev/null 2>&1 || true
  else
    log "Installing graphify..."
  fi
  uv tool install graphifyy
  graphify_works && log "graphify installed: $(graphify --version)" || log "graphify still not working — see output above"
else
  log "graphify already installed: $(graphify --version)"
fi

# Graphify integration.
#
# САМ СКИЛЛ `/graphify` больше здесь не раскладывается: он лежит в платформенном
# слое (skills/graphify) и приезжает в проект общим механизмом — значит виден
# Claude, OpenCode, Codex И Hermes разом, попадает в индекс AGENTS.skills.md и
# обновляется централизованно. Раньше его писал сам CLI в ~/.claude/skills и
# ~/.agents/skills: это home, свой у каждого проекта, мимо Hermes и мимо индекса.
#
# Из инсталлера нужен ровно один артефакт — opencode-плагин (always-on хук), это
# не скилл, а рантайм-хук. Гоним из временной директории: инсталлер всегда
# создаёт <cwd>/.opencode/, а мусорить им в репозитории не хотим.
#
# Скилл в платформе версионирован вместе с CLI (skills/graphify/.graphify_version).
# Разъезд версий не ломает работу, но текст скилла может отстать от CLI —
# предупреждаем, чтобы обновили в платформе осознанно.
if command -v graphify >/dev/null 2>&1; then
  # корень платформы — от расположения самого скрипта (работает и из
  # /opt/ai-devcontainer/tooling в проекте, и из tooling/ в репо платформы)
  SKILL_VER_FILE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/skills/graphify/.graphify_version"
  if [ -f "$SKILL_VER_FILE" ]; then
    want="$(cat "$SKILL_VER_FILE")"
    have="$(graphify --version 2>/dev/null | awk '{print $NF}')"
    if [ -n "$have" ] && [ "$want" != "$have" ]; then
      log "  ! скилл graphify в платформе собран под $want, установлен CLI $have"
      log "    обновить: graphify install --platform claude && cp -a ~/.claude/skills/graphify <клон платформы>/skills/"
    fi
  fi
  log "Installing graphify opencode plugin..."
  gtmp="$(mktemp -d)"
  (
    cd "$gtmp"
    graphify install --platform opencode >/dev/null 2>&1 || log "  → opencode (skipped/failed)"
  )
  # opencode always-on хук: инсталлер кладёт его в <cwd>/.opencode/plugins/.
  # Переносим в глобальный ~/.config/opencode/plugins/ (auto-load, bind на хост):
  # так хук работает во всех проектах и не попадает в репозиторий.
  if [ -f "$gtmp/.opencode/plugins/graphify.js" ]; then
    mkdir -p "$HOME/.config/opencode/plugins"
    cp "$gtmp/.opencode/plugins/graphify.js" "$HOME/.config/opencode/plugins/graphify.js"
    log "  → opencode hook → ~/.config/opencode/plugins/graphify.js"
  fi
  rm -rf "$gtmp"
else
  log "graphify not installed, skipping opencode plugin"
fi

# Strix — автономные агенты для security-тестирования (pip-пакет strix-agent).
# Не MCP-сервер, а самостоятельный CLI: поднимает песочницу в Docker, гоняет в
# ней разведку и эксплуатацию, каждую находку подтверждает PoC. Ставим тем же
# uv tool в volume /opt/ai-tools, что и graphify, — переживает rebuild.
#
# Требует ДВУХ вещей, которых у нас может не быть, и обе проверяются в рантайме,
# а не здесь: работающий docker-демон и свой LLM-ключ (STRIX_LLM + LLM_API_KEY).
# Поэтому установка не падает и ничего не настраивает — как пользоваться,
# описано в скилле senior-security.
strix_works() {
  command -v strix >/dev/null 2>&1 && strix --version >/dev/null 2>&1
}

if ! strix_works; then
  if command -v strix >/dev/null 2>&1; then
    log "Found broken strix — reinstalling..."
    uv tool uninstall strix-agent >/dev/null 2>&1 || true
  else
    log "Installing strix..."
  fi
  uv tool install strix-agent || log "strix install failed — не блокирует (ставится вручную: uv tool install strix-agent)"
  strix_works && log "strix installed: $(strix --version)" || log "strix not available — см. вывод выше"
else
  log "strix already installed: $(strix --version)"
fi

# Skaffold
if [ ! -x "$AI_TOOLS_HOME/bin/skaffold" ]; then
  log "Installing skaffold..."
  curl -Lo "$AI_TOOLS_HOME/bin/skaffold" \
    https://storage.googleapis.com/skaffold/releases/latest/skaffold-linux-amd64
  chmod +x "$AI_TOOLS_HOME/bin/skaffold"
else
  log "skaffold already installed"
fi

log "AI tools setup complete."