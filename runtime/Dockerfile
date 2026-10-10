# syntax=docker/dockerfile:1
FROM golang:1.26.1-bookworm AS adc-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY bin/ bin/
ARG ADC_REVISION=unknown
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-X github.com/foxford/ai-devcontainer/bin/adc/internal.Revision=${ADC_REVISION}" -o /adc ./bin/adc

FROM node:24.18.0-bookworm-slim AS node-toolchain
FROM debian:bookworm-slim
ENV DEBIAN_FRONTEND=noninteractive

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
      fzf \
    && rm -rf /var/lib/apt/lists/*

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      libnss3 libnspr4 \
      libatk1.0-0 libatk-bridge2.0-0 libatspi2.0-0 \
      libcups2 libdrm2 libgbm1 \
      libxkbcommon0 libxcomposite1 libxdamage1 libxfixes3 libxrandr2 \
      libpango-1.0-0 libcairo2 libasound2 \
      fonts-liberation fonts-noto-color-emoji fonts-unifont \
    && rm -rf /var/lib/apt/lists/*

RUN sed -i 's/^# ru_RU.UTF-8/ru_RU.UTF-8/' /etc/locale.gen && \
    sed -i 's/^# en_US.UTF-8/en_US.UTF-8/' /etc/locale.gen && \
    locale-gen
ENV LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8

RUN groupadd --gid 1000 node && \
    useradd --uid 1000 --gid 1000 --create-home --shell /usr/bin/zsh node && \
    echo 'node ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/node && \
    chmod 0440 /etc/sudoers.d/node

ENV RUSTUP_HOME=/usr/local/rustup CARGO_HOME=/usr/local/cargo
ENV PATH=/usr/local/cargo/bin:${PATH}
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | \
      sh -s -- -y --no-modify-path --profile default && \
    chmod -R a+rwX "$RUSTUP_HOME" "$CARGO_HOME"

ENV BUN_INSTALL=/usr/local/bun
ENV PATH=/usr/local/bun/bin:${PATH}
RUN curl -fsSL https://bun.sh/install | bash && \
    chmod -R a+rwX "$BUN_INSTALL"

ENV ASDF_DATA_DIR=/home/node/.asdf
ENV AI_TOOLS_HOME=/opt/ai-tools
ENV NPM_CONFIG_PREFIX=/home/node/.npm-global
ENV CLAUDE_CONFIG_DIR=/home/node/.claude
ENV GRAPHIFY_VIZ_NODE_LIMIT=50000
ENV PLAYWRIGHT_BROWSERS_PATH=/home/node/.cache/ms-playwright
ENV EDITOR=nano VISUAL=nano SHELL=/usr/bin/zsh
ENV PATH=/home/node/.asdf/shims:/home/node/.asdf/bin:/opt/ai-tools/bin:/home/node/.npm-global/bin:/home/node/.local/bin:${PATH}

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

# Electron and PTY dependencies for the packaged Orca server.
RUN apt-get update && apt-get install -y --no-install-recommends \
    xvfb libgtk-3-0 libxtst6 libxrender1 libx11-xcb1 libxcb-dri3-0 libxss1 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=node-toolchain /usr/local/ /usr/local/
COPY --from=adc-build /usr/local/go/ /usr/local/go/
# Node's Yarn links target /opt in its source image; Corepack owns them here.
RUN rm -f /usr/local/bin/yarn /usr/local/bin/yarnpkg && corepack enable

USER node
ARG ZSH_IN_DOCKER_VERSION=1.2.1
RUN curl -fL --retry 5 --retry-all-errors --retry-max-time 180 \
      --connect-timeout 20 --max-time 60 -o /tmp/zsh-in-docker.sh \
      "https://github.com/deluan/zsh-in-docker/releases/download/v${ZSH_IN_DOCKER_VERSION}/zsh-in-docker.sh" && \
    test -s /tmp/zsh-in-docker.sh && \
    mkdir -p /tmp/adc-zsh-curl && \
    printf '%s\n' 'fail' 'connect-timeout = 20' 'max-time = 60' \
      > /tmp/adc-zsh-curl/.curlrc && \
    CURL_HOME=/tmp/adc-zsh-curl sh /tmp/zsh-in-docker.sh \
      -p git -p docker -p npm \
      -p https://github.com/zsh-users/zsh-autosuggestions \
      -p https://github.com/zsh-users/zsh-completions \
      -x && test -d /home/node/.oh-my-zsh && \
    rm -f /tmp/zsh-in-docker.sh /tmp/adc-zsh-curl/.curlrc && rmdir /tmp/adc-zsh-curl
USER root
# A migrated, already populated home volume hides all image home contents.
RUN mkdir -p /opt/adc-shell && \
    cp -a /home/node/.oh-my-zsh /home/node/.zshrc /home/node/.bashrc /opt/adc-shell/
# Login shells also restore the toolchain and agent adapter paths.
RUN printf '%s\n' '. /opt/ai-devcontainer/runtime/shell-env.sh' > /etc/profile.d/99-adc.sh
COPY runtime/orca-lock.json /tmp/orca-lock.json
# Fixed artifacts and checksum; unpack once without FUSE. Platform itself is
# mounted read-only, not baked into this image.
RUN arch="$(dpkg --print-architecture)" && \
    url="$(jq -r --arg target "linux/$arch" '.assets[$target].url' /tmp/orca-lock.json)" && \
    digest="$(jq -r --arg target "linux/$arch" '.assets[$target].sha256' /tmp/orca-lock.json)" && \
    test "$url" != null && mkdir -p /opt/orca && cd /opt/orca && \
    curl -fL --retry 3 "$url" -o orca.AppImage && \
    printf '%s  orca.AppImage\n' "$digest" | sha256sum -c - && \
    chmod +x orca.AppImage && ./orca.AppImage --appimage-extract >/dev/null && \
    chmod -R a+rX squashfs-root && rm orca.AppImage /tmp/orca-lock.json && \
    ln -s /opt/orca/squashfs-root/resources/bin/orca-ide /usr/local/bin/orca
COPY --from=adc-build /adc /usr/local/bin/adc

RUN mkdir -p /opt/adc-agents && \
    for agent in claude codex opencode hermes dsh; do \
      ln -s /usr/local/bin/adc /opt/adc-agents/$agent; \
    done
ENV ADC_RUNTIME=1 ADC_HOME=/var/lib/adc AI_DEVCONTAINER_HOME=/opt/ai-devcontainer \
    ADC_WORKSPACE=developer AI_TOOLS_HOME=/opt/ai-tools \
    PATH=/opt/adc-agents:/home/node/.asdf/shims:/home/node/.asdf/bin:/opt/ai-tools/bin:/home/node/.npm-global/bin:/home/node/.local/bin:/usr/local/go/bin:/usr/local/cargo/bin:/usr/local/bun/bin:/usr/local/bin:/usr/sbin:/usr/bin:/bin \
    LIBGL_ALWAYS_SOFTWARE=1
WORKDIR /workspace
ENTRYPOINT ["adc", "runtime-serve"]
