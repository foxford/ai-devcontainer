#!/usr/bin/env bash
set -euo pipefail

# ─── Цвета ────────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

ASDF_VERSION="v0.18.0"

# ─── Вспомогательные функции ──────────────────────────────────────────────────

info()    { echo -e "${BLUE}[INFO]${NC} $*"; }
success() { echo -e "${GREEN}[OK]${NC} $*"; }
warn()    { echo -e "${YELLOW}[WARN]${NC} $*"; }
error()   { echo -e "${RED}[ERROR]${NC} $*" >&2; }

die() {
  error "$@"
  exit 1
}

check_command() {
  command -v "$1" &>/dev/null
}

# ─── Определение ОС ──────────────────────────────────────────────────────────

detect_os() {
  case "$(uname -s)" in
    Darwin) echo "macos" ;;
    Linux)  echo "linux" ;;
    *)      die "Неподдерживаемая ОС: $(uname -s). Поддерживаются macOS и Linux." ;;
  esac
}

# ─── Определение корня репозитория ────────────────────────────────────────────

# Скрипт живёт в образе (/opt/dev-tooling), а НЕ внутри репо — поэтому корень
# репозитория берём из текущего каталога вызова (или из $REPO_ROOT), а не от
# расположения файла.
REPO_ROOT="${REPO_ROOT:-$PWD}"

# ─── Установка asdf ──────────────────────────────────────────────────────────

install_asdf() {
  if check_command asdf; then
    success "asdf уже установлен: $(asdf version)"
    return 0
  fi

  local os
  os="$(detect_os)"

  info "Устанавливаю asdf ${ASDF_VERSION}..."

  if [[ "$os" == "macos" ]]; then
    if ! check_command brew; then
      die "Homebrew не найден. Установите: https://brew.sh"
    fi
    brew install asdf
  elif [[ "$os" == "linux" ]]; then
    local arch
    arch="$(uname -m)"
    case "$arch" in
      x86_64)  arch="amd64" ;;
      aarch64) arch="arm64" ;;
      *)       die "Неподдерживаемая архитектура: $arch" ;;
    esac

    local url="https://github.com/asdf-vm/asdf/releases/download/${ASDF_VERSION}/asdf-${ASDF_VERSION}-linux-${arch}.tar.gz"
    local tmp_dir="/tmp/asdf-dl-$$"
    mkdir -p "$tmp_dir"

    info "Скачиваю asdf ${ASDF_VERSION} (linux-${arch})..."
    curl -fsSL "$url" | tar xz -C "$tmp_dir"

    mkdir -p "$HOME/.local/bin"
    cp "$tmp_dir/asdf" "$HOME/.local/bin/asdf"
    rm -rf "$tmp_dir"

    if ! echo "$PATH" | grep -q "$HOME/.local/bin"; then
      warn "\$HOME/.local/bin не в \$PATH. Добавьте в ваш shell-профиль:"
      warn "  export PATH=\"\$HOME/.local/bin:\$PATH\""
      export PATH="$HOME/.local/bin:$PATH"
    fi
  fi

  if ! check_command asdf; then
    die "asdf не удалось установить. Проверьте вывод выше."
  fi

  success "asdf установлен: $(asdf version)"
}

# ─── Установка Node.js через asdf ────────────────────────────────────────────

install_node_via_asdf() {
  local node_version="$1"

  # Добавляем плагин nodejs если отсутствует
  if ! asdf plugin list 2>/dev/null | grep -q '^nodejs$'; then
    info "Добавляю плагин nodejs в asdf..."
    asdf plugin add nodejs
  fi

  # Устанавливаем Node.js если не установлен
  if asdf list nodejs 2>/dev/null | grep -q "$node_version"; then
    success "Node.js $node_version уже установлен через asdf"
  else
    info "Устанавливаю Node.js $node_version через asdf (это может занять несколько минут)..."
    asdf install nodejs "$node_version"
  fi

  (cd "$REPO_ROOT" && asdf reshim nodejs)
}

# ─── Обеспечение нужной версии Node.js ────────────────────────────────────────

ensure_node() {
  local required_version
  required_version="$(grep '^nodejs' "$REPO_ROOT/.tool-versions" 2>/dev/null | awk '{print $2}' || true)"

  if [[ -z "$required_version" ]]; then
    die "Не удалось определить версию Node.js из .tool-versions"
  fi

  info "Требуемая версия Node.js: $required_version"

  # Проверяем, что node идёт из asdf (путь содержит .asdf)
  local node_path
  node_path="$(which node 2>/dev/null || true)"
  if [[ -n "$node_path" && "$node_path" != *".asdf"* ]]; then
    warn "node найден не из asdf: $node_path"
    warn "Сделайте asdf главнее в PATH (в ~/.zshrc или ~/.bashrc загружайте asdf в конце или добавьте .asdf/shims в начало PATH)."
  fi

  # Проверяем текущую версию Node
  local current_version=""
  if check_command node; then
    current_version="$(node --version 2>/dev/null | sed 's/^v//')"
  fi

  if [[ "$current_version" == "$required_version" ]]; then
    success "Node.js $required_version уже установлен"
    return 0
  fi

  if [[ -n "$current_version" ]]; then
    warn "Текущая версия Node.js: $current_version (требуется $required_version)"
  else
    info "Node.js не найден"
  fi

  # Node не той версии или отсутствует → устанавливаем через asdf
  install_asdf
  echo ""
  install_node_via_asdf "$required_version"

  success "Node.js готов: $(node --version)"
}

# ─── Настройка corepack + pnpm ───────────────────────────────────────────────

setup_pnpm() {
  local pnpm_version
  pnpm_version="$(node -e "
    const pkg = require('$REPO_ROOT/package.json');
    const pm = pkg.packageManager || '';
    const match = pm.match(/^pnpm@([^+]+)/);
    if (match) console.log(match[1]);
  ")"

  if [[ -z "$pnpm_version" ]]; then
    die "Не удалось определить версию pnpm из package.json (поле packageManager)"
  fi

  info "Требуемая версия pnpm: $pnpm_version"

  # Node 25+ больше не включает corepack в дистрибутив — доставляем его через npm.
  # Он ставится в писабельный $NPM_CONFIG_PREFIX (asdf сам реиндексирует шимы),
  # поэтому enable/prepare проходят без плясок с правами. Дальше pnpm ставит и
  # пинит сам corepack по полю packageManager.
  command -v corepack >/dev/null 2>&1 || npm install -g corepack
  corepack enable
  corepack prepare "pnpm@$pnpm_version" --activate

  success "pnpm готов: $(pnpm --version)"
}

# ─── Установка зависимостей ───────────────────────────────────────────────────

install_dependencies() {
  info "Устанавливаю зависимости проекта (pnpm install)..."
  (cd "$REPO_ROOT" && pnpm install)
  success "Зависимости установлены"
}

# ─── Основной поток ──────────────────────────────────────────────────────────

main() {
  local env_only=false

  for arg in "$@"; do
    case "$arg" in
      --env-only) env_only=true ;;
      -h|--help)
        echo "Использование: setup.sh [--env-only]"
        echo ""
        echo "  --env-only  Только настройка окружения (Node + pnpm), без pnpm install."
        echo "              Используется внутри Docker build."
        exit 0
        ;;
      *) die "Неизвестный аргумент: $arg. Используйте --help." ;;
    esac
  done

  echo ""
  echo -e "${BLUE}╔══════════════════════════════════════════════════╗${NC}"
  echo -e "${BLUE}║  Dev Container — Настройка окружения             ║${NC}"
  echo -e "${BLUE}╚══════════════════════════════════════════════════╝${NC}"
  echo ""

  info "ОС: $(detect_os) ($(uname -s) $(uname -m))"
  info "Корень репозитория: $REPO_ROOT"
  if [[ "$env_only" == true ]]; then
    info "Режим: --env-only (без pnpm install)"
  fi
  echo ""

  ensure_node
  echo ""
  setup_pnpm
  echo ""
  install_global_npm_tools

  if [[ "$env_only" == false ]]; then
    echo ""
    install_dependencies
  fi

  echo ""
  echo -e "${GREEN}╔══════════════════════════════════════════════════╗${NC}"
  echo -e "${GREEN}║  Окружение готово к работе!                      ║${NC}"
  echo -e "${GREEN}╚══════════════════════════════════════════════════╝${NC}"
  echo ""
  info "Node.js: $(node --version)"
  info "pnpm:    $(pnpm --version)"
  echo ""
}

# ─── Установка глобальных npm CLI пакетов ─────────────────────────────────────

install_global_npm_tools() {
  local packages=(zx tsx)

  info "Устанавливаю глобальные npm пакеты: ${packages[*]}"

  # Убедимся, что глобальный prefix доступен на запись.
  # Если нет — переключим на ~/.local (не требует sudo).
  local prefix
  prefix="$(npm config get prefix 2>/dev/null || true)"

  if [[ -z "$prefix" || ! -w "$prefix" ]]; then
    warn "npm prefix '$prefix' недоступен на запись. Переключаю prefix на \$HOME/.local"
    npm config set prefix "$HOME/.local"

    # PATH: чтобы глобальные бинарники находились
    if ! echo "$PATH" | grep -q "$HOME/.local/bin"; then
      warn "\$HOME/.local/bin не в PATH. Добавьте в shell-профиль:"
      warn "  export PATH=\"\$HOME/.local/bin:\$PATH\""
      export PATH="$HOME/.local/bin:$PATH"
    fi
  fi

  # Ставим пакеты
  npm i -g "${packages[@]}"

  success "Глобальные npm пакеты установлены"
}

main "$@"
