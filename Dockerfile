# syntax=docker/dockerfile:1
# base/Dockerfile — образ dev-base: общий тулчейн для всех проектов группы.
#
# Что здесь ЕСТЬ (одинаково для всех проектов):
#   • системные пакеты, локали, zsh + oh-my-zsh, юзер node, sudo
#   • rust, bun, go
#   • системные зависимости headless-chromium (сами браузеры — нет, см. ниже)
#   • предсозданные каталоги под bind/volume-маунты (иначе Docker создаст их от
#     root и postCreate упадёт на первом mkdir в ~/.local — EACCES)
#   • скрипты тулинга в /opt/dev-tooling (+ seed-шаблоны .hermes/skills)
#
# Чего здесь НЕТ (проектное, ставится в Dockerfile проекта):
#   • Node и pnpm — их версии берутся из .tool-versions и package.json ПРОЕКТА
#     через `bash /opt/dev-tooling/setup.sh --env-only`
#   • браузеры Playwright — их ревизия привязана к версии пакета в ПРОЕКТЕ,
#     а образ общий; едут в named volume, ставит post-create-setup.sh
#
# База — голый debian-slim, а НЕ node:*-slim: node-образ несёт свою ноду и
# corepack в root-овском /usr/local/bin, а asdf ставит вторую ноду из
# .tool-versions — и базовый corepack от юзера node не может писать в системный
# bin (EACCES). Голая база = единственная нода (asdf), единственный источник
# версии (.tool-versions).

ARG DEBIAN_SUITE=bookworm
FROM debian:${DEBIAN_SUITE}-slim

# Имя проекта используется только в приглашении/баннере и подставляется на
# РАНТАЙМЕ из env — поэтому в базе это просто дефолт, а проект задаёт своё.
ARG PROJECT_NAME=Dev
ENV PROJECT_NAME=${PROJECT_NAME}
ENV DEBIAN_FRONTEND=noninteractive

# ── Системные пакеты ──────────────────────────────────────────────
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      ca-certificates curl wget \
      git openssh-client \
      sudo \
      build-essential make pkg-config \
      unzip \
      jq yq \
      python3 \
      htop procps \
      nmap dnsutils mtr net-tools \
      zsh less \
      locales \
      ripgrep \
      rsync \
      ffmpeg \
      nano \
      golang \
      fzf \
    && rm -rf /var/lib/apt/lists/*

# ── Системные зависимости headless-chromium (Playwright) ──────────
# Сами БРАУЗЕРЫ здесь не ставим: их ревизия привязана к версии пакета
# playwright, а образ один на все проекты — прибитый сюда chromium разъехался
# бы с `@playwright/test` в репозиториях. Браузеры едут в named volume
# (PLAYWRIGHT_BROWSERS_PATH ниже), их ставит post-create-setup.sh.
# В образе — только apt-часть: она от версии playwright не зависит, а без неё
# chromium падает на старте с невнятным "Target closed".
#
# Список = вывод `playwright install-deps --dry-run chromium` на bookworm за
# вычетом xvfb/x11-стека: дисплея в контейнере нет, гоняем только headless.
# Шрифты нужны не для запуска, а для картинки: без них скриншоты и снапшоты
# агента приезжают с квадратами вместо кириллицы и эмодзи.
# Обновить список: `npx playwright install-deps --dry-run chromium` в контейнере.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      libnss3 libnspr4 \
      libatk1.0-0 libatk-bridge2.0-0 libatspi2.0-0 \
      libcups2 libdrm2 libgbm1 \
      libxkbcommon0 libxcomposite1 libxdamage1 libxfixes3 libxrandr2 \
      libpango-1.0-0 libcairo2 libasound2 \
      fonts-liberation fonts-noto-color-emoji fonts-unifont \
    && rm -rf /var/lib/apt/lists/*

# ── Локали UTF-8 (ru + en) ────────────────────────────────────────
RUN sed -i 's/^# ru_RU.UTF-8/ru_RU.UTF-8/' /etc/locale.gen && \
    sed -i 's/^# en_US.UTF-8/en_US.UTF-8/' /etc/locale.gen && \
    locale-gen
ENV LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8

# ── Юзер node (uid/gid 1000 — совпадает с типичным хостовым, чтобы
#    bind-маунты не имели проблем с правами) + sudo без пароля ───────
RUN groupadd --gid 1000 node && \
    useradd --uid 1000 --gid 1000 --create-home --shell /usr/bin/zsh node && \
    echo 'node ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/node && \
    chmod 0440 /etc/sudoers.d/node

# ── Rust ───────────────────────────────────────────────────────────
ENV RUSTUP_HOME=/usr/local/rustup CARGO_HOME=/usr/local/cargo
ENV PATH=/usr/local/cargo/bin:${PATH}
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | \
      sh -s -- -y --no-modify-path --profile default && \
    chmod -R a+rwX "$RUSTUP_HOME" "$CARGO_HOME"

# ── Bun ────────────────────────────────────────────────────────────
ENV BUN_INSTALL=/usr/local/bun
ENV PATH=/usr/local/bun/bin:${PATH}
RUN curl -fsSL https://bun.sh/install | bash && \
    chmod -R a+rwX "$BUN_INSTALL"

# ── Env окружения и AI-инструментов ────────────────────────────────
# asdf-шимы идут ПЕРВЫМИ в PATH — это единственный источник node/npm/pnpm.
# CLAUDE_CONFIG_DIR уводит настройки Claude Code (включая ~/.claude.json, который
# лежит файлом РЯДОМ с ~/.claude) внутрь смонтированного каталога — иначе они не
# переживают rebuild.
ENV ASDF_DATA_DIR=/home/node/.asdf
ENV AI_TOOLS_HOME=/opt/ai-tools
ENV NPM_CONFIG_PREFIX=/home/node/.npm-global
ENV CLAUDE_CONFIG_DIR=/home/node/.claude
ENV GRAPHIFY_VIZ_NODE_LIMIT=50000
# Браузеры Playwright — вне образа, в named volume (см. skeleton devcontainer).
# Путь совпадает с дефолтом playwright, но задан явно: на него смотрят и
# раннер проекта, и браузерный MCP, и маунт в devcontainer.json.
ENV PLAYWRIGHT_BROWSERS_PATH=/home/node/.cache/ms-playwright
ENV EDITOR=nano VISUAL=nano SHELL=/usr/bin/zsh
ENV PATH=/home/node/.asdf/shims:/home/node/.asdf/bin:/opt/ai-tools/bin:/home/node/.npm-global/bin:/home/node/.local/bin:${PATH}

# ── Каталоги под маунты (node:node — иначе Docker создаст от root) ──
# Не формальность: проверено — named volume на несуществующий в образе путь
# монтируется root:root, и юзер node в него уже не запишет.
RUN mkdir -p \
      /home/node/.pnpm-store \
      /home/node/.claude \
      /home/node/.config/opencode \
      /home/node/.codex \
      /home/node/.hermes \
      /home/node/.dsh \
      /home/node/.cache/ms-playwright \
      /home/node/.local/share/claude \
      /home/node/.local/share/uv \
      /home/node/.local/bin \
      /home/node/.npm-global \
      /home/node/.history \
      /home/node/.asdf \
      /opt/ai-tools/bin && \
    chown -R node:node /home/node /opt/ai-tools

# ── setup.sh — ЕДИНСТВЕННЫЙ запечённый скрипт ──────────────────────
# Он нужен на этапе СБОРКИ проектного образа (asdf/node/pnpm), когда маунтов
# ещё нет. Всё остальное (post-create, helpers, seed, скиллы) приезжает в
# рантайме маунтом клона платформы → /opt/ai-devcontainer (см. skeleton
# devcontainer.json) — правка скриптов НЕ требует пересборки образа.
COPY --chown=node:node tooling/setup.sh /opt/dev-tooling/setup.sh
RUN chmod +x /opt/dev-tooling/setup.sh

USER node
WORKDIR /workspaces

# ── zsh: oh-my-zsh + powerlevel10k, история на персистентный путь ──
# ВАЖНО: скрипт качаем в файл и проверяем результат. Паттерн sh -c "$(wget -O- …)"
# при сбое сети даёт sh -c "" → шаг «успешен», а oh-my-zsh молча не установлен
# (голый PS1 в терминале). Плюс явная проверка каталога после установки.
ARG ZSH_IN_DOCKER_VERSION=1.2.1
RUN wget -O /tmp/zsh-in-docker.sh \
      "https://github.com/deluan/zsh-in-docker/releases/download/v${ZSH_IN_DOCKER_VERSION}/zsh-in-docker.sh" && \
    test -s /tmp/zsh-in-docker.sh && \
    sh /tmp/zsh-in-docker.sh \
      -p git \
      -p docker \
      -p npm \
      -p https://github.com/zsh-users/zsh-autosuggestions \
      -p https://github.com/zsh-users/zsh-completions \
      -a "export HISTFILE=/home/node/.history/.zsh_history" \
      -a "export HISTSIZE=50000" \
      -a "export SAVEHIST=50000" \
      -a "setopt INC_APPEND_HISTORY SHARE_HISTORY HIST_IGNORE_DUPS HIST_IGNORE_SPACE" \
      -x && \
    test -d /home/node/.oh-my-zsh && \
    rm -f /tmp/zsh-in-docker.sh

# ── Общий env для zsh И bash (AI-тулзы часто спавнят bash) ──────────
# Отдельными RUN на каждый файл: `cat >> a <<'RC' && cat >> b <<'RC'` с одним
# телом — ловушка: второй heredoc получает EOF и b остаётся пустым.
RUN cat >> /home/node/.zshrc <<'RC'

# ── dev-container env (источник: ai-devcontainer/Dockerfile) ──
export ASDF_DATA_DIR=/home/node/.asdf
export AI_TOOLS_HOME=/opt/ai-tools
export NPM_CONFIG_PREFIX=$HOME/.npm-global
export CLAUDE_CONFIG_DIR=/home/node/.claude
export GRAPHIFY_VIZ_NODE_LIMIT=50000
export PLAYWRIGHT_BROWSERS_PATH=/home/node/.cache/ms-playwright
export PATH=$HOME/.asdf/shims:$HOME/.asdf/bin:$AI_TOOLS_HOME/bin:$HOME/.npm-global/bin:$HOME/.local/bin:/usr/local/cargo/bin:/usr/local/bun/bin:$PATH
RC
RUN cat >> /home/node/.bashrc <<'RC'

# ── dev-container env (источник: ai-devcontainer/Dockerfile) ──
export ASDF_DATA_DIR=/home/node/.asdf
export AI_TOOLS_HOME=/opt/ai-tools
export NPM_CONFIG_PREFIX=$HOME/.npm-global
export CLAUDE_CONFIG_DIR=/home/node/.claude
export GRAPHIFY_VIZ_NODE_LIMIT=50000
export PLAYWRIGHT_BROWSERS_PATH=/home/node/.cache/ms-playwright
export PATH=$HOME/.asdf/shims:$HOME/.asdf/bin:$AI_TOOLS_HOME/bin:$HOME/.npm-global/bin:$HOME/.local/bin:/usr/local/cargo/bin:/usr/local/bun/bin:$PATH
RC

# ── bash-история на персистентный путь ─────────────────────────────
RUN cat >> /home/node/.bashrc <<'RC'
export HISTFILE=/home/node/.history/.bash_history
export HISTSIZE=50000
export HISTFILESIZE=50000
shopt -s histappend
PROMPT_COMMAND="history -a;${PROMPT_COMMAND}"
RC

# ── Баннер (имя проекта подставляется на рантайме из $PROJECT_NAME) ─
RUN cat >> /home/node/.zshrc <<'RC'

# Префикс к PS1 — идемпотентно: VS Code shell integration может пере-сорсить
# .zshrc, без guard'а префикс дублируется ([bunker] [bunker] …).
case "$PS1" in *"[${PROJECT_NAME}]"*) ;; *)
  export PS1="%{$fg[cyan]%}[${PROJECT_NAME}]%{$reset_color%} $PS1" ;;
esac
# Текст приветствия НЕ печём в образ — берём из монтируемого клона платформы
# (tooling/motd.sh), чтобы его правка не требовала пересборки dev-base. Здесь
# только поиск файла и фоллбек на одну строку, если платформа не примонтирована.
if [ -t 1 ]; then
  _motd=""
  for _c in /opt/ai-devcontainer/tooling/motd.sh "$PWD/tooling/motd.sh"; do
    if [ -r "$_c" ]; then _motd="$_c"; break; fi
  done
  if [ -n "$_motd" ]; then
    PROJECT_NAME="$PROJECT_NAME" sh "$_motd"
  else
    echo "🤖 ${PROJECT_NAME} Dev Container"
  fi
  unset _motd _c
fi
RC

# ── Login-shell env (/etc/profile.d) ───────────────────────────────
# bash -l / zsh -l читают /etc/profile, который перезатирает PATH дефолтным
# значением и роняет asdf-шимы/npm-global. rc-файлы не спасают: .bashrc в
# неинтерактивном шелле выходит по guard'у до наших export'ов.
RUN sudo tee /etc/profile.d/00-devcontainer.sh >/dev/null <<'RC'
export ASDF_DATA_DIR="$HOME/.asdf"
export AI_TOOLS_HOME=/opt/ai-tools
export NPM_CONFIG_PREFIX="$HOME/.npm-global"
export CLAUDE_CONFIG_DIR="$HOME/.claude"
export GRAPHIFY_VIZ_NODE_LIMIT=50000
export PLAYWRIGHT_BROWSERS_PATH="$HOME/.cache/ms-playwright"
export PATH="$HOME/.asdf/shims:$HOME/.asdf/bin:$AI_TOOLS_HOME/bin:$HOME/.npm-global/bin:$HOME/.local/bin:/usr/local/cargo/bin:/usr/local/bun/bin:$PATH"
RC
