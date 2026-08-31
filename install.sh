#!/usr/bin/env bash
# install.sh — бутстрап ai-devcontainer на машине разработчика.
#
#   curl -fsSL https://raw.githubusercontent.com/foxford/ai-devcontainer/main/install.sh | bash
#
# Делает:
#   1. клонирует (или обновляет) платформу в ~/.local/share/ai-devcontainer
#   2. кладёт CLI `ai-devcontainer` в ~/.local/bin
#   3. создаёт персист-каталоги ~/.ai-devcontainer-dev/*
#
# Переопределения:
#   AI_DEVCONTAINER_REPO — URL/путь репозитория (default ниже; локальный путь тоже работает)
#   AI_DEVCONTAINER_HOME — куда ставить (default ~/.local/share/ai-devcontainer)

set -euo pipefail

REPO="${AI_DEVCONTAINER_REPO:-git@github.com:foxford/ai-devcontainer.git}"
DEST="${AI_DEVCONTAINER_HOME:-$HOME/.local/share/ai-devcontainer}"
BIN_DIR="$HOME/.local/bin"

C_GREEN='\033[0;32m'; C_YELLOW='\033[0;33m'; C_RESET='\033[0m'
log()  { echo -e "${C_GREEN}==>${C_RESET} $*"; }
warn() { echo -e "${C_YELLOW}!! ${C_RESET}$*" >&2; }

command -v git >/dev/null 2>&1 || { echo "git не найден — поставь и повтори" >&2; exit 1; }

# ── 0. preflight хоста ──────────────────────────────────────────
# ОС и пакетный менеджер — по тому же принципу, что detect_os() в
# tooling/setup.sh (тот слой ставит Node/pnpm ВНУТРИ образа при build; этот —
# то, что нужно ДО докера, на голом хосте).
detect_os() {
  case "$(uname -s)" in
    Darwin) echo "macos" ;;
    Linux)  echo "linux" ;;
    *)      echo "unknown" ;;
  esac
}

# WSL — тот же linux-путь установки; отдельная пометка нужна только для
# докер-подсказки ниже (Docker Desktop с WSL2-backend, не системный docker).
is_wsl() { uname -r 2>/dev/null | grep -qiE 'microsoft|wsl'; }

detect_pkg_mgr() {
  if command -v brew >/dev/null 2>&1; then echo brew
  elif command -v apt-get >/dev/null 2>&1; then echo apt
  elif command -v dnf >/dev/null 2>&1; then echo dnf
  elif command -v pacman >/dev/null 2>&1; then echo pacman
  fi
}

# Лёгкие зависимости — ставим молча, без запроса подтверждения (так же тихо,
# как tooling/setup.sh уже ставит asdf).
ensure_host_tool() {
  local bin="$1" apt_pkg="$2" brew_pkg="$3" dnf_pkg="$4" pacman_pkg="$5"
  command -v "$bin" >/dev/null 2>&1 && { log "$bin уже есть"; return 0; }

  local mgr pkg=""
  mgr="$(detect_pkg_mgr)"
  case "$mgr" in
    brew)   pkg="$brew_pkg" ;;
    apt)    pkg="$apt_pkg" ;;
    dnf)    pkg="$dnf_pkg" ;;
    pacman) pkg="$pacman_pkg" ;;
  esac

  if [ -z "$mgr" ] || [ -z "$pkg" ]; then
    warn "$bin не найден, пакетный менеджер не определён — поставь вручную"
    return 0
  fi

  log "Ставлю $bin ($mgr install $pkg)..."
  case "$mgr" in
    brew)   brew install "$pkg" ;;
    apt)    sudo apt-get update -qq && sudo apt-get install -y "$pkg" ;;
    dnf)    sudo dnf install -y "$pkg" ;;
    pacman) sudo pacman -Sy --noconfirm "$pkg" ;;
  esac
}

# Docker — НЕ так тихо, как rsync/jq: на Linux меняет группы пользователя
# (нужен перелогин), на macOS первый запуск Docker.app требует ручного
# system-level разрешения — headless это сделать нельзя, только предупредить.
ensure_docker() {
  if command -v docker >/dev/null 2>&1; then
    if docker info >/dev/null 2>&1; then
      log "docker уже есть и запущен"
    else
      warn "docker установлен, но демон не отвечает — запусти Docker Desktop или \`sudo systemctl start docker\`"
    fi
    return 0
  fi

  case "$(detect_os)" in
    macos)
      if ! command -v brew >/dev/null 2>&1; then
        warn "docker не найден, Homebrew тоже — поставь Docker Desktop вручную: https://www.docker.com/products/docker-desktop"
        return 0
      fi
      log "Ставлю Docker Desktop (brew install --cask docker)..."
      brew install --cask docker
      warn "Открой Docker.app один раз вручную — первый запуск просит system-level разрешение, headless это сделать нельзя"
      ;;
    linux)
      log "Ставлю docker (get.docker.com)..."
      curl -fsSL https://get.docker.com | sh
      sudo usermod -aG docker "$USER"
      warn "Добавлен в группу docker — перелогинься или выполни \`newgrp docker\`, чтобы это применилось без sudo"
      if is_wsl; then
        warn "WSL: нужен Docker Desktop на хосте с включённым WSL2-backend для этого дистрибутива (Settings → Resources → WSL Integration)"
      fi
      ;;
    *)
      warn "Не распознал ОС ($(uname -s)) — поставь docker вручную"
      ;;
  esac
}

log "ОС: $(detect_os)$(is_wsl && echo ' (WSL)')"
ensure_host_tool rsync rsync rsync rsync rsync
ensure_host_tool jq jq jq jq jq
ensure_docker

# ── 1. клон/обновление платформы ───────────────────────────────
if [ -d "$DEST/.git" ]; then
  log "Платформа уже есть — обновляю: $DEST"
  git -C "$DEST" pull --ff-only
else
  log "Клонирую $REPO → $DEST"
  mkdir -p "$(dirname "$DEST")"
  git clone --depth 1 "$REPO" "$DEST"
fi

# ── 2. CLI на PATH ─────────────────────────────────────────────
mkdir -p "$BIN_DIR"
ln -sfn "$DEST/bin/ai-devcontainer" "$BIN_DIR/ai-devcontainer"
ln -sfn "$DEST/bin/ai-devcontainer" "$BIN_DIR/adc"
chmod +x "$DEST/bin/ai-devcontainer"
log "CLI: $BIN_DIR/ai-devcontainer (короткий алиас: adc)"

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) warn "~/.local/bin не в PATH — добавь в свой rc: export PATH=\"\$HOME/.local/bin:\$PATH\"";;
esac

# ── 3. корень персиста ─────────────────────────────────────────
# Каталоги — PER-PROJECT (~/.ai-devcontainer-dev/<проект>/*), их создаёт
# initializeCommand самого проекта. Здесь только корень.
mkdir -p "$HOME/.ai-devcontainer-dev"
log "Персист: ~/.ai-devcontainer-dev/<проект>/* (создаются при первом открытии проекта)"

echo ""
log "Готово. Проверяю окружение:"
echo ""
"$BIN_DIR/ai-devcontainer" doctor || true

echo ""
log "Дальше:"
echo "      ai-devcontainer new my-service    # завести проект"
echo "      # образ dev-base:local соберётся сам при первом Reopen in Container"
