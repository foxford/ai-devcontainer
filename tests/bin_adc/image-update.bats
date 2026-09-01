#!/usr/bin/env bats
# bin/adc — cmd_ensure_image / cmd_update / host_only.

setup() {
  load '../bats/lib/bats-support/load'
  load '../bats/lib/bats-assert/load'
  load '../helpers/fixtures'
  load '../helpers/mocks'

  BIN="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)/bin/adc"
  PLATFORM_FIXTURE="$(make_platform_fixture)"
  mocks_init
}

teardown() {
  chmod -R +w "$PLATFORM_FIXTURE" 2>/dev/null || true
  cleanup_fixture "$PLATFORM_FIXTURE"
  mocks_cleanup
}

run_bin() {
  AI_DEVCONTAINER_HOME="$PLATFORM_FIXTURE" run bash "$BIN" "$@"
}

@test "ensure-image: заблокировано, если PLATFORM_ROOT read-only" {
  chmod -w "$PLATFORM_FIXTURE"
  run_bin ensure-image
  assert_failure
  assert_output --partial "доступна только на хосте"
}

# Мок docker: image inspect отдаёт $1 как метку образа (пусто — образа нет),
# buildx inspect — драйвер сборки, context show — имя контекста.
mock_docker() {
  local label="${1:-}" driver="${2:-docker}"
  cat > "$MOCK_BIN_DIR/docker" <<EOF
#!/usr/bin/env bash
echo "\$@" >> "$MOCK_CALLS_DIR/docker.log"
case "\$1 \$2" in
  "image inspect") [ -n "$label" ] || exit 1; echo "$label"; exit 0 ;;
  "buildx inspect") echo "Name:   test"; echo "Driver: $driver"; exit 0 ;;
  "context show")   echo "desktop-linux"; exit 0 ;;
esac
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/docker"
}

@test "ensure-image: образ актуален (label совпадает) — не пересобирает" {
  local key
  key="$(cat "$PLATFORM_FIXTURE/Dockerfile" "$PLATFORM_FIXTURE/tooling/setup.sh" | sha256sum | cut -c1-12)"
  mock_docker "$key"

  run_bin ensure-image
  assert_success
  assert_output --partial "актуален"
  run cat "$MOCK_CALLS_DIR/docker.log"
  # Именно `build`, а не любое вхождение: в логе есть ещё `buildx inspect` —
  # проверка окружения сборки, она как раз должна была случиться.
  refute_output --regexp '(^| )build '
  assert_output --partial "buildx inspect"
}

@test "ensure-image: образа нет — собирает" {
  mock_docker ""

  run_bin ensure-image
  assert_success
  assert_output --partial "Готово: dev-base:local"
  run cat "$MOCK_CALLS_DIR/docker.log"
  assert_output --partial "build"
}

# dev-base:local нигде не публикуется, поэтому FROM обязан резолвиться из
# локального хранилища демона. Билдер docker-container его не видит, и проект
# падает с «pull access denied: docker.io/library/dev-base:local» — ошибкой, по
# которой сходу не догадаться, что виноват билдер.
@test "ensure-image: билдер docker-container — предупреждение про pull access denied" {
  local key
  key="$(cat "$PLATFORM_FIXTURE/Dockerfile" "$PLATFORM_FIXTURE/tooling/setup.sh" | sha256sum | cut -c1-12)"
  mock_docker "$key" docker-container

  run_bin ensure-image
  assert_success
  assert_output --partial "не видит локальные образы"
  assert_output --partial "docker buildx use default"
}

@test "ensure-image: обычный драйвер docker — показывает контекст, без паники" {
  local key
  key="$(cat "$PLATFORM_FIXTURE/Dockerfile" "$PLATFORM_FIXTURE/tooling/setup.sh" | sha256sum | cut -c1-12)"
  mock_docker "$key" docker

  run_bin ensure-image
  assert_success
  assert_output --partial "контекст desktop-linux"
  refute_output --partial "не видит локальные образы"
}

# bin/adc — единственное, что платформа запускает НА ХОСТЕ, а хост бывает
# macOS: там нет sha256sum, `readlink -f` появился только в 12.3, а BSD `sed -i`
# требует аргумент-суффикс. Под `set -e` любая такая дыра убивает `adc prepare`,
# образ не собирается — и человек видит постороннее «pull access denied» на
# FROM dev-base:local. Тест сторожит именно повторный занос GNU-изма в CLI:
# отловить его на Linux-прогоне иначе нечем.
@test "portability: в bin/adc нет GNU-only конструкций" {
  # Комментарии не считаем: в них эти имена стоят как раз с объяснением, почему
  # их тут нет. Смотрим только исполняемый код.
  # Строка самого фолбэка (`command -v sha256sum … then sha256sum`) — не нарушение:
  # это и есть переносимая обёртка, ради которой всё затевалось.
  run bash -c "grep -vE '^[[:space:]]*#' '$BIN' | grep -v 'command -v sha256sum' \
                 | grep -nE 'sha256sum|readlink -f|sed -i |stat -c|find [^|]*-printf|grep -P'"
  assert_failure   # ни одного совпадения
}

# adc почти всегда зовут через симлинк (~/.local/bin/adc → клон платформы), и
# корень платформы вычисляется разыменованием ЭТОГО пути. Без `readlink -f`
# логика самописная — проверяем, что она работает.
@test "portability: запуск через симлинк находит корень платформы" {
  local key link_dir
  key="$(cat "$PLATFORM_FIXTURE/Dockerfile" "$PLATFORM_FIXTURE/tooling/setup.sh" | sha256sum | cut -c1-12)"
  mock_docker "$key" docker
  link_dir="$(mktemp -d)"
  ln -s "$BIN" "$link_dir/adc"

  AI_DEVCONTAINER_HOME="$PLATFORM_FIXTURE" run bash "$link_dir/adc" ensure-image
  assert_success
  assert_output --partial "актуален"
  rm -rf "$link_dir"
}

@test "update: PLATFORM_ROOT read-only — делегирует в sync" {
  # Заглушки для sub-скриптов, которые cmd_sync зовёт — до chmod -w.
  printf '#!/usr/bin/env bash\ntrue\n' > "$PLATFORM_FIXTURE/tooling/skill.sh"
  printf '#!/usr/bin/env bash\ntrue\n' > "$PLATFORM_FIXTURE/tooling/wire-mcp.sh"
  chmod +x "$PLATFORM_FIXTURE/tooling/skill.sh" "$PLATFORM_FIXTURE/tooling/wire-mcp.sh"
  REPO_DIR="$(make_repo_fixture)"
  chmod -w "$PLATFORM_FIXTURE"

  AI_DEVCONTAINER_HOME="$PLATFORM_FIXTURE" REPO_ROOT="$REPO_DIR" run bash "$BIN" update
  assert_success
  assert_output --partial "read-only"
  cleanup_fixture "$REPO_DIR"
}

@test "update: writable, не git-клон — предупреждает, но пересобирает" {
  cat > "$MOCK_BIN_DIR/docker" <<EOF
#!/usr/bin/env bash
[ "\$1" = "image" ] && [ "\$2" = "inspect" ] && exit 1
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/docker"
  run_bin update
  assert_success
  assert_output --partial "не git-клон, пропускаю pull"
}

# «Образ есть, а девконтейнер его не видит»: docker image inspect отвечает за
# демона, а базу ищет BuildKit. Поэтому doctor повторяет ровно ту сборку,
# которую сделает VS Code, и показывает её настоящую ошибку.
@test "doctor: FROM dev-base:local не резолвится — жалуется, а не молчит" {
  cat > "$MOCK_BIN_DIR/docker" <<'EOF'
#!/usr/bin/env bash
case "$1 $2" in
  "image inspect")  echo somekey; exit 0 ;;
  "buildx inspect") echo "Driver: docker"; exit 0 ;;
  "context show")   echo desktop-linux; exit 0 ;;
  "build -q")
    echo "ERROR: failed to solve: pull access denied, repository does not exist" >&2
    exit 1 ;;
esac
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/docker"
  run_bin doctor
  assert_output --partial "не резолвится"
  assert_output --partial "pull access denied"
}

# ── диагностика сети: хостовая половина ───────────────────────
# В контейнере виден только симптом (крупный ответ висит), а причина — здесь:
# docker раздаёт контейнерам MTU больше, чем канал наружу.
mock_ip() {
  local dev="${1:-eth0}" mtu="${2:-1500}"
  cat > "$MOCK_BIN_DIR/ip" <<EOF
#!/usr/bin/env bash
case "\$*" in
  *"route get"*) echo "1.1.1.1 via 10.0.0.1 dev $dev src 10.0.0.2"; exit 0 ;;
  *"link show"*) echo "2: $dev: <BROADCAST,MULTICAST,UP> mtu $mtu qdisc fq_codel"; exit 0 ;;
esac
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/ip"
}

# Мок docker, у которого ещё и bridge отдаёт заданный MTU.
mock_docker_with_bridge() {
  local key="$1" bridge_mtu="$2"
  cat > "$MOCK_BIN_DIR/docker" <<EOF
#!/usr/bin/env bash
case "\$1 \$2" in
  "image inspect")   echo "$key"; exit 0 ;;
  "buildx inspect")  echo "Driver: docker"; exit 0 ;;
  "context show")    echo default; exit 0 ;;
  "network inspect") echo "$bridge_mtu"; exit 0 ;;
esac
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/docker"
}

@test "doctor на хосте: MTU докера больше канала — это и есть причина зависаний" {
  mock_ip eth0 1400
  mock_docker_with_bridge somekey 1500
  run_bin doctor
  assert_output --partial "docker раздаёт контейнерам MTU 1500, а канал наружу — 1400"
  assert_output --partial '{"mtu": 1400}'
}

@test "doctor на хосте: MTU совпадают — короткая строка без паники" {
  mock_ip eth0 1500
  mock_docker_with_bridge somekey 1500
  run_bin doctor
  assert_output --partial "MTU 1500 ≤ канал 1500"
  refute_output --partial "будут висеть"
}

@test "doctor на хосте: MTU канала не определить — говорит прямо, не гадает" {
  printf '#!/usr/bin/env bash\nexit 1\n' > "$MOCK_BIN_DIR/ip"
  printf '#!/usr/bin/env bash\nexit 1\n' > "$MOCK_BIN_DIR/route"
  chmod +x "$MOCK_BIN_DIR/ip" "$MOCK_BIN_DIR/route"
  mock_docker_with_bridge somekey 1500
  run_bin doctor
  assert_output --partial "MTU канала не определить"
}

# ── диагностика сети контейнера ───────────────────────────────
# В контейнере (PLATFORM_ROOT read-only) doctor проверяет сеть лесенкой
# резолв → мелкий HTTPS → крупный HTTPS. Провалившаяся ступень = диагноз;
# самая коварная — последняя: мелкое ходит, крупное висит, и это MTU.
run_doctor_in_container() {
  chmod -w "$PLATFORM_FIXTURE"
  AI_DEVCONTAINER_HOME="$PLATFORM_FIXTURE" run bash "$BIN" doctor
}

@test "doctor в контейнере: крупный HTTPS висит — диагноз MTU, а не «интернет тормозит»" {
  mock_bin getent 0 ""
  cat > "$MOCK_BIN_DIR/curl" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  *registry.npmjs.org*) exit 28 ;;   # 28 = таймаут curl
esac
exit 0
EOF
  chmod +x "$MOCK_BIN_DIR/curl"
  run_doctor_in_container
  assert_output --partial "MTU: мелкий ответ прошёл, крупный завис"
  assert_output --partial '{"mtu": 1400}'
  assert_output --partial "расширений VS Code"
}

@test "doctor в контейнере: не резолвится DNS — упирается в резолвер, дальше не идёт" {
  mock_bin getent 1 ""
  mock_bin curl 0 ""
  run_doctor_in_container
  assert_output --partial "не резолвится"
  refute_output --partial "MTU: мелкий ответ"
}

@test "doctor в контейнере: сеть в порядке — так и говорит" {
  mock_bin getent 0 ""
  mock_bin curl 0 ""
  run_doctor_in_container
  assert_output --partial "DNS, TLS и крупный ответ — все три прошли"
}
