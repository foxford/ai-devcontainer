# syntax=docker/dockerfile:1
# Девконтейнер САМОЙ платформы: правим skeleton/tooling/Dockerfile в песочнице,
# агенты (claude/codex/hermes) работают внутри контейнера.
#
# База — тот же dev-base:local, но initializeCommand собирает его из РАБОЧЕЙ
# КОПИИ (AI_DEVCONTAINER_HOME=$PWD), а не из ~/.local/share/ai-devcontainer: правки
# Dockerfile/setup.sh должны попадать в образ, который сейчас тестируешь.

FROM dev-base:local

ARG PROJECT_NAME=ai-devcontainer
ENV PROJECT_NAME=${PROJECT_NAME}

# Нода и pnpm — по манифестам skeleton (это и есть разрабатываемая монорепа)
COPY --chown=node:node skeleton/package.json skeleton/.tool-versions /tmp/repo/
RUN cd /tmp/repo && \
    bash /opt/dev-tooling/setup.sh --env-only && \
    cp /tmp/repo/.tool-versions /home/node/.tool-versions && \
    rm -rf /tmp/repo
