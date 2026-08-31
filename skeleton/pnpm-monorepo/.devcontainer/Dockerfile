# syntax=docker/dockerfile:1
# Тонкий проектный Dockerfile: весь тулчейн — в dev-base (см. репо ai-devcontainer).
# Здесь только проектное: нода из .tool-versions и pnpm из package.json.
#
# Образ собирается ЛОКАЛЬНО из клона платформы (~/.local/share/ai-devcontainer):
# initializeCommand ниже в devcontainer.json дособерёт его при необходимости.
# Обновление окружения = adc update (pull платформы + пересборка).

FROM dev-base:local

ARG PROJECT_NAME=MyProject
ENV PROJECT_NAME=${PROJECT_NAME}

# Нода и pnpm этого проекта (слой пересобирается только при смене манифестов)
COPY --chown=node:node package.json .tool-versions /tmp/repo/
RUN cd /tmp/repo && \
    bash /opt/dev-tooling/setup.sh --env-only && \
    cp /tmp/repo/.tool-versions /home/node/.tool-versions && \
    rm -rf /tmp/repo
# /home/node/.tool-versions — глобальный дефолт Node: без него asdf-шим падает
# ("No version set") в каталогах без .tool-versions (например /workspaces).
