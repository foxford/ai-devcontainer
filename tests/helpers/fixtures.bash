# Билдеры временных деревьев для тестов. load из .bats: load '../helpers/fixtures'

# Каталог-репозиторий с .git/ (для repo_root()-style walk-up поиска).
# make_repo_fixture [--devcontainer] -> печатает путь к каталогу.
make_repo_fixture() {
  local dir; dir="$(mktemp -d)"
  mkdir -p "$dir/.git"
  if [ "${1:-}" = "--devcontainer" ]; then
    mkdir -p "$dir/.devcontainer"
    cat > "$dir/.devcontainer/devcontainer.json" <<'EOF'
{
  "name": "fixture",
  "initializeCommand": "ai-devcontainer prepare || \"$HOME/.local/bin/ai-devcontainer\" prepare"
}
EOF
  fi
  echo "$dir"
}

# Минимальный клон структуры платформы: skeleton/<type>/, skills/, mcp/servers.json.
# make_platform_fixture -> печатает путь к каталогу.
make_platform_fixture() {
  local dir; dir="$(mktemp -d)"
  mkdir -p "$dir/skeleton/pnpm-monorepo/.devcontainer"
  cat > "$dir/skeleton/pnpm-monorepo/.scaffold.json" <<'EOF'
{
  "label": "Node/TS монорепа (тестовая фикстура)",
  "rename": [
    { "file": "package.json", "pattern": "\"my-project\"", "replace": "\"{{NAME}}\"" },
    { "file": ".devcontainer/devcontainer.json", "pattern": "MyProject", "replace": "{{NAME}}" },
    { "file": ".devcontainer/Dockerfile", "pattern": "MyProject", "replace": "{{NAME}}" }
  ]
}
EOF
  echo '{"name": "my-project"}' > "$dir/skeleton/pnpm-monorepo/package.json"
  cat > "$dir/skeleton/pnpm-monorepo/.devcontainer/devcontainer.json" <<'EOF'
{
  "name": "MyProject",
  "initializeCommand": "ai-devcontainer prepare || \"$HOME/.local/bin/ai-devcontainer\" prepare"
}
EOF
  echo "FROM scratch" > "$dir/skeleton/pnpm-monorepo/.devcontainer/Dockerfile"
  mkdir -p "$dir/skills"
  mkdir -p "$dir/mcp"
  echo '{"mcpServers": {}}' > "$dir/mcp/servers.json"
  mkdir -p "$dir/tooling"
  # PLATFORM_ROOT в bin/ai-devcontainer откатывается на реальный репозиторий,
  # если тут нет Dockerfile — держим фикстуру самодостаточной.
  echo "FROM debian:bookworm-slim" > "$dir/Dockerfile"
  echo "#!/usr/bin/env bash" > "$dir/tooling/setup.sh"
  echo "$dir"
}

cleanup_fixture() {
  [ -n "${1:-}" ] && [ -d "$1" ] && rm -rf "$1"
}
