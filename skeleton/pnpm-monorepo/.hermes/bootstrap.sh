#!/usr/bin/env bash
# .hermes/bootstrap.sh — установка одной командой для новых членов команды.
#
# Идемпотентно: запускать можно сколько угодно раз.
#
# Шаги:
#   1. Проверить предусловия (node, pnpm, hermes, jq, yq). Включить pnpm через corepack.
#   2. Поставить зависимости и собрать packages/hermes-process через pnpm
#   3. Создать Kanban-доску под проект
#   4. Установить 6 профилей в ~/.hermes/profiles/
#   5. Зарегистрировать .agents/skills/ в ~/.hermes/config.yaml
#   6. Установить gateway-хук с подстановкой PROJECT_ROOT
#   7. Подключить шелл-хук валидации pre_tool_call
#   8. Напечатать дальнейшие шаги

set -euo pipefail

# ── пути ─────────────────────────────────────────────────────
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
PROJECT_ROOT="$( cd "$SCRIPT_DIR/.." >/dev/null 2>&1 && pwd )"
HERMES_DIR="$SCRIPT_DIR"
# Каталог скиллов для Hermes — собранное overlay-дерево (платформенные скиллы
# + проектные форки поверх), его раскладывает wire-agent-skills.sh на шаге 6
# post-create. Именно оно полное: .agents/skills держит только отличия проекта
# и как источник для Hermes неполон. Фоллбек — на ручной запуск bootstrap до wire.
# Определяем здесь: skill_universe() ниже использует это ещё до секции 5.
SKILLS_PATH="$PROJECT_ROOT/.claude/skills"
[ -d "$SKILLS_PATH" ] || SKILLS_PATH="$PROJECT_ROOT/.agents/skills"
# hermes-process может жить двумя способами:
#   • workspace-пакет packages/hermes-process (репо самой ai-devcontainer);
#   • установленная зависимость @foxford/hermes-process (обычный проект,
#     пакет приезжает из GitLab-registry уже СОБРАННЫМ — build/ в тарболе).
PROCESS_PKG="$PROJECT_ROOT/packages/hermes-process"
PROCESS_IN_MODULES="$PROJECT_ROOT/node_modules/@foxford/hermes-process"
if [ -d "$PROCESS_PKG" ]; then
  BUNDLE_PATH="$PROCESS_PKG/build/state-machine/index.js"
  PROCESS_MODE="workspace"
else
  BUNDLE_PATH="$PROCESS_IN_MODULES/build/state-machine/index.js"
  PROCESS_MODE="dependency"
fi
HERMES_HOME="$HOME/.hermes"

PROJECT_SLUG="$(basename "$PROJECT_ROOT" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9-_' '-' | sed 's/-*$//' | sed 's/^-*//')"
BOARD_SLUG="${HERMES_BOARD_OVERRIDE:-$PROJECT_SLUG}"

# ── цвета ────────────────────────────────────────────────────
C_GREEN='\033[0;32m'; C_YELLOW='\033[0;33m'; C_RED='\033[0;31m'
C_DIM='\033[2m';     C_RESET='\033[0m'
log()  { echo -e "${C_GREEN}==>${C_RESET} $*"; }
warn() { echo -e "${C_YELLOW}!!${C_RESET}  $*" >&2; }
err()  { echo -e "${C_RED}xx${C_RESET}  $*" >&2; }
dim()  { echo -e "${C_DIM}$*${C_RESET}"; }

# ── баннер ───────────────────────────────────────────────────
echo ""
log "Бутстрап пайплайна Hermes"
dim "Корень проекта: $PROJECT_ROOT"
dim "Слаг проекта:   $PROJECT_SLUG"
dim "Слаг доски:     $BOARD_SLUG"
dim "Пакет process:  $PROCESS_PKG"
echo ""

# ── 1. предусловия ───────────────────────────────────────────

ensure_node() {
  if ! command -v node >/dev/null 2>&1; then
    err "Node.js не найден. Поставь Node.js >= 20 (через nvm/fnm или пакетный менеджер дистрибутива)."
    exit 1
  fi
  local v="$(node --version)"
  dim "  node:   $v"
}

ensure_pnpm() {
  if command -v pnpm >/dev/null 2>&1; then
    dim "  pnpm:   $(pnpm --version)"
    return
  fi
  if command -v corepack >/dev/null 2>&1; then
    log "Включаю pnpm через corepack…"
    if corepack enable >/dev/null 2>&1; then
      if ! command -v pnpm >/dev/null 2>&1; then
        corepack prepare pnpm@latest --activate >/dev/null 2>&1 || true
      fi
    fi
  fi
  if ! command -v pnpm >/dev/null 2>&1; then
    err "pnpm не найден, и corepack не смог его включить."
    err "Попробуй:  corepack enable && corepack prepare pnpm@latest --activate"
    exit 1
  fi
  dim "  pnpm:   $(pnpm --version)"
}

ensure_command() {
  local cmd="$1" install_hint="$2"
  if command -v "$cmd" >/dev/null 2>&1; then
    dim "  $cmd: $(command -v "$cmd")"
    return
  fi
  err "Команда '$cmd' не найдена."
  err ""
  err "$install_hint"
  exit 1
}


log "Проверяю предусловия"

ensure_node
ensure_pnpm

ensure_command hermes "Поставь Hermes Agent:
  https://hermes-agent.nousresearch.com/docs/getting-started/quickstart"

ensure_command jq "Поставь jq:
  Linux (apt):       sudo apt update && sudo apt install -y jq
  macOS (brew):      brew install jq"

ensure_command yq "Поставь yq:
  Linux (apt):       sudo apt update && sudo apt install -y yq
  macOS (brew):      brew install yq"

ensure_command python3 "Поставь Python 3:
  Linux (apt):       sudo apt update && sudo apt install -y python3
  macOS (brew):      brew install python"

if [ ! -d "$PROCESS_PKG" ]; then
  err "Пакет не найден: $PROCESS_PKG"
  exit 1
fi

mkdir -p "$HERMES_HOME"/{hooks,profiles,skills,logs,state-machine}
log "Предусловия в порядке"

# ── 2. зависимости + сборка hermes-process ───────────────────
log "Ставлю зависимости pnpm-воркспейса"
( cd "$PROJECT_ROOT" && pnpm install --frozen-lockfile=false --silent ) || \
  { [ "$PROCESS_MODE" = "workspace" ] && ( cd "$PROCESS_PKG" && pnpm install --silent ); }

if [ "$PROCESS_MODE" = "workspace" ]; then
  log "Собираю hermes-process (pnpm --filter @foxford/hermes-process build)"
  pnpm --filter @foxford/hermes-process build
else
  log "hermes-process — установленная зависимость, сборка не нужна ($PROCESS_IN_MODULES)"
fi

if [ ! -f "$BUNDLE_PATH" ]; then
  err "Бандл не найден: $BUNDLE_PATH"
  if [ "$PROCESS_MODE" = "workspace" ]; then
    err "Запусти 'pnpm --filter @foxford/hermes-process build' вручную."
  else
    err "Добавь @foxford/hermes-process в devDependencies проекта и повтори pnpm install."
  fi
  exit 1
fi
log "Собрано: $BUNDLE_PATH ($(du -h "$BUNDLE_PATH" | cut -f1))"

# ── 3. Kanban-доска под проект ───────────────────────────────
if hermes kanban boards list 2>/dev/null | awk '{print $1}' | grep -qx "$BOARD_SLUG"; then
  dim "Kanban-доска '$BOARD_SLUG' уже есть"
else
  log "Создаю Kanban-доску '$BOARD_SLUG'"
  hermes kanban boards create "$BOARD_SLUG" --name "$PROJECT_SLUG" --description "Доска пайплайна для $PROJECT_SLUG"
fi
HERMES_KANBAN_BOARD="$BOARD_SLUG" hermes kanban init >/dev/null 2>&1 || true

# ── 4. установка профилей ────────────────────────────────────
#
# Профиль Hermes — это изолированный home-каталог в ~/.hermes/profiles/<role>/.
# Все скиллы из skills.external_dirs (наш .agents/skills/) видны КАЖДОМУ профилю-
# клону. Сужение по роли делается выключением: имена в массиве skills.disabled
# профиля выпадают из индекса скиллов системного промпта
# (agent.skill_utils.get_disabled_skill_names).
#
# ЧТО включить у роли — allow-list в .hermes/profile-skills.yaml (источник правды).
# bootstrap раскладывает его в skills.disabled ОДИН раз, при первом запуске
# (маркер skills.foxford_seed). Дальше разработчик правит живьём через дашборд-
# плагин profile-skills, и повторный bootstrap его НЕ перетирает.

declare_description() {
  case "$1" in
    team-lead)        echo "Разбивает фичереквесты на подзадачи реализации по плану tech-lead. Единственная роль с kanban_create." ;;
    tech-lead)        echo "Пишет планы, ревьюит код, маршрутизирует после QA, делает финальную проверку. Многошаговая." ;;
    developer)        echo "Пишет код и unit/component-тесты в worktree фичи. Никогда не пушит." ;;
    qa)               echo "Пишет e2e/интеграционные тесты против AC. Не трогает продакшен-код." ;;
    documenter)       echo "Обновляет документацию. Тот же worktree, не пушит." ;;
    security-auditor) echo "Read-only OWASP-аудит по флагу needs_security." ;;
  esac
}

# Печатает JSON-массив из аргументов: a b c -> ["a","b","c"] (пусто -> []).
to_json_array() {
  local out="" s
  for s in "$@"; do out="$out\"$s\","; done
  printf '[%s]' "${out%,}"
}

# Allow-list скиллов по ролям — источник правды для сида (см. комментарий выше).
SKILL_SCOPES="$HERMES_DIR/profile-skills.yaml"

# Вселенная скиллов на машине — каноничные ИМЕНА (frontmatter `name`, fallback —
# имя каталога) из всех мест, что видит индекс Hermes: локальный ~/.hermes/skills +
# наши external_dirs. ВАЖНО: матчить нужно по `name`, а не по имени каталога — у
# части встроенных скиллов они различаются (audiocraft → audiocraft-audio-generation,
# vllm → serving-llms-vllm и т.п.); иначе disabled не сработает и счётчик в плагине
# уйдёт в минус. Дедуп по имени (как в индексе). disabled роли = вселенная − allow-list.
skill_universe() {
  python3 - "$HERMES_HOME/skills" "$SKILLS_PATH" "$PROJECT_ROOT/.hermes/skills" <<'PY'
import sys, os, glob, re

# Зеркало skill_matches_platform из agent/skill_utils.py: скилл с frontmatter
# `platforms: [...]`, несовместимый с текущей ОС, в индекс НЕ попадает. Не
# включаем такие во вселенную, иначе disabled вылезет за реальный набор и
# счётчик в плагине уйдёт в минус (на Linux так отсекаются macOS-скиллы).
PMAP = {"macos": "darwin", "linux": "linux", "windows": "win32"}
def platform_ok(pl):
    if not pl:
        return True
    if not isinstance(pl, list):
        pl = [pl]
    cur = sys.platform
    return any(cur.startswith(PMAP.get(str(p).lower().strip(), str(p).lower().strip())) for p in pl)

try:
    import yaml
    def frontmatter(text):
        if not text.startswith("---"):
            return {}
        try:
            return yaml.safe_load(text[3:text.index("\n---", 3)]) or {}
        except Exception:
            return {}
except Exception:
    # Без PyYAML — без фильтра платформы (деградация), имя берём регэкспом.
    def frontmatter(text):
        fm = {}
        if text.startswith("---"):
            for ln in text.splitlines()[1:]:
                if ln.strip() == "---":
                    break
                m = re.match(r"\s*name\s*:\s*(.+?)\s*$", ln)
                if m:
                    fm["name"] = m.group(1).strip().strip('"').strip("'")
        return fm

names = set()
for base in sys.argv[1:]:
    if not os.path.isdir(base):
        continue
    for p in glob.glob(os.path.join(base, "**", "SKILL.md"), recursive=True):
        nm = os.path.basename(os.path.dirname(p))  # fallback — имя каталога
        try:
            fm = frontmatter(open(p, encoding="utf-8").read())
        except Exception:
            fm = {}
        if not platform_ok(fm.get("platforms")):
            continue
        names.add(str(fm.get("name") or nm).strip() or nm)
print("\n".join(sorted(names)))
PY
}
SKILL_UNIVERSE="$(skill_universe)"

log "Устанавливаю профили"
for profile_dir in "$HERMES_DIR"/profiles/*/; do
  [ -d "$profile_dir" ] || continue
  role="$(basename "$profile_dir")"

  # 1. Создаём профиль через --clone (наследует config.yaml + .env + SOUL.md от
  #    текущего активного профиля, обычно это default, настроенный разработчиком
  #    через `hermes setup`). Описание использует kanban-декомпозер Hermes.
  if hermes profile list 2>/dev/null | awk '{print $1}' | grep -qx "$role"; then
    dim "  - $role (есть)"
    # Обновляем описание у существующего профиля, если команда доступна
    hermes profile describe "$role" "$(declare_description "$role")" >/dev/null 2>&1 || true
  else
    log "  Создаю профиль '$role' (клон активного)"
    hermes profile create "$role" \
      --clone \
      --description "$(declare_description "$role")" \
      >/dev/null 2>&1 || hermes profile create "$role" --clone >/dev/null 2>&1 || true
  fi

  # ВАЖНО: наш SOUL.md перезапишет тот, что скопировал --clone, и это то, что
  # нам нужно: клонированный SOUL.md — это персона default, а не наша роль.

  dest="$HERMES_HOME/profiles/$role"

  # 2. Копируем SOUL.md (имя файла, которое ждёт Hermes)
  cp "$profile_dir/SOUL.md" "$dest/SOUL.md"

  # 3. Сид скоупинга скиллов роли — ТОЛЬКО при первом запуске.
  #
  #    Источник правды — allow-list роли в $SKILL_SCOPES: перечислено = включено,
  #    всё прочее из вселенной выключаем (skills.disabled). Сид одноразовый:
  #    маркер skills.foxford_seed помечает, что мы уже разложили базлайн. Если
  #    маркер есть — НЕ трогаем: дальше скоупинг живёт в дашборде profile-skills.
  #
  #    Маркер нужен, потому что --clone наследует skills.disabled активного
  #    профиля, так что «нет ключа disabled» — ненадёжный признак первого запуска;
  #    наш маркер клон не наследует (его нет у дефолтного профиля).
  config="$dest/config.yaml"
  [ -s "$config" ] || echo "{}" > "$config"

  seeded="$(yq -r '.skills.foxford_seed // ""' "$config" 2>/dev/null || true)"
  if [ -n "$seeded" ]; then
    dim "      скиллы уже сижены — не трогаю (правь через дашборд profile-skills)"
  elif [ ! -f "$SKILL_SCOPES" ]; then
    warn "Нет $SKILL_SCOPES — сид скиллов для '$role' пропущен"
  else
    allowed="$(yq -r ".profiles[\"$role\"][]?" "$SKILL_SCOPES" 2>/dev/null || true)"
    disabled_for_role=""
    for skill in $SKILL_UNIVERSE; do
      is_allowed=0
      for a in $allowed; do [ "$skill" = "$a" ] && { is_allowed=1; break; }; done
      [ "$is_allowed" -eq 0 ] && disabled_for_role="$disabled_for_role $skill"
    done
    yq -y -i \
      ".skills.disabled = $(to_json_array $disabled_for_role) | .skills.foxford_seed = true" \
      "$config"
    dim "      сид: включено $(echo $allowed | wc -w), выключено $(echo $disabled_for_role | wc -w), маркер выставлен"
  fi
done
log "Профили установлены: $HERMES_HOME/profiles/"

# ── 5. skills.external_dirs ──────────────────────────────────
CONFIG="$HERMES_HOME/config.yaml"
# Флаг -i у yq не может прочитать полностью пустой файл (jq под капотом давится
# пустым вводом). Засеваем {}, если файла нет или он пуст.
if [ ! -s "$CONFIG" ]; then
  echo "{}" > "$CONFIG"
fi
log "Регистрирую каталог скиллов: $SKILLS_PATH"
yq -y -i ".skills.external_dirs = ((.skills.external_dirs // []) + [\"$SKILLS_PATH\"] | unique)" "$CONFIG"

# ── 6. gateway-хук (с подстановкой) ──────────────────────────
HOOK_SRC="$HERMES_DIR/hermes-hooks/state-machine-launcher"
HOOK_DST="$HERMES_HOME/hooks/state-machine-launcher-$PROJECT_SLUG"
mkdir -p "$HOOK_DST"

cp "$HOOK_SRC/HOOK.yaml" "$HOOK_DST/HOOK.yaml"
sed \
  -e "s|{{PROJECT_ROOT}}|$PROJECT_ROOT|g" \
  -e "s|{{BOARD_SLUG}}|$BOARD_SLUG|g" \
  -e "s|{{PROJECT_SLUG}}|$PROJECT_SLUG|g" \
  "$HOOK_SRC/handler.py" > "$HOOK_DST/handler.py"
log "Gateway-хук установлен: $HOOK_DST"

# ── 7. шелл-хук валидации ────────────────────────────────────
VALIDATOR="$HERMES_DIR/hermes-hooks/validation/validate-handoff.sh"
chmod +x "$VALIDATOR"

log "Подключаю хук валидации pre_tool_call"
yq -y -i "
  .hooks.pre_tool_call =
    ((.hooks.pre_tool_call // [])
      | map(select(.command != \"$VALIDATOR\"))
      + [{\"matcher\": \"kanban_(complete|create|link|unblock)\", \"command\": \"$VALIDATOR\", \"timeout\": 5}])
" "$CONFIG"

yq -y -i ".hooks_auto_accept = true" "$CONFIG"

# ── 7b. плагины дашборда ─────────────────────────────────────
# Дашборд Hermes ищет плагины по пути
#   ~/.hermes/plugins/<name>/dashboard/manifest.json
# Плагин теперь собирается общим build пакета hermes-process (шаг 2) в
# build/plugins/profile-skills/ вместе с manifest.json и plugin_api.py. Симлинкуем
# собранный каталог на место — скан обнаружения считает симлинк обычным каталогом.
PLUGIN_DIR="$PROCESS_PKG/build/plugins/profile-skills"
PLUGINS_HOME="$HERMES_HOME/plugins"
mkdir -p "$PLUGINS_HOME"
if [ -d "$PLUGIN_DIR/dashboard" ]; then
  ln -sfn "$PLUGIN_DIR" "$PLUGINS_HOME/profile-skills"
  log "Плагин дашборда слинкован: $PLUGINS_HOME/profile-skills -> $PLUGIN_DIR"
  dim "      перезапусти дашборд, чтобы смонтировать роуты /api/plugins/profile-skills/"
else
  warn "Сборка плагина дашборда отсутствует: $PLUGIN_DIR/dashboard (запусти 'pnpm --filter @foxford/hermes-process build')"
fi

# ── 7c. темы дашборда ────────────────────────────────────────
# Дашборд ищет пользовательские темы в ~/.hermes/dashboard-themes/*.yaml
# (hermes_cli.web_server._discover_user_themes). Исходники тем держим в монорепе
# и симлинкуем каждый .yaml на место: репо остаётся источником правды
# (обновления через `git pull`), а чужие пользовательские темы в домашней папке
# не трогаются.
THEMES_SRC="$HERMES_DIR/dashboard-themes"
THEMES_DST="$HERMES_HOME/dashboard-themes"
if [ -d "$THEMES_SRC" ] && compgen -G "$THEMES_SRC/*.yaml" >/dev/null; then
  mkdir -p "$THEMES_DST"
  theme_count=0
  for theme in "$THEMES_SRC"/*.yaml; do
    ln -sfn "$theme" "$THEMES_DST/$(basename "$theme")"
    theme_count=$((theme_count + 1))
  done
  log "Темы дашборда слинкованы: $theme_count → $THEMES_DST"
  dim "      перезапусти дашборд, чтобы подхватить новые или изменённые темы"
else
  dim "Тем дашборда в $THEMES_SRC нет (пропущено)"
fi

# ── 8. итог ──────────────────────────────────────────────────
echo ""
log "Бутстрап завершён"
echo ""
echo "  Доска:            $BOARD_SLUG"
echo "  Бандл:            $BUNDLE_PATH"
echo "  Профили:          $HERMES_HOME/profiles/{team-lead,tech-lead,developer,qa,documenter,security-auditor}"
echo "  State machine:    поднимает хук gateway:startup (node $BUNDLE_PATH)"
echo "  Валидация:        pre_tool_call → $VALIDATOR"
echo "  Скиллы:           $SKILLS_PATH"
echo "  Плагин дашборда:  $PLUGINS_HOME/profile-skills (вкладка Profile Skills + API)"
echo ""
echo "Дальше:"
echo ""
echo "  1. Скиллы уже приехали из платформы (см. AGENTS.skills.md);"
echo "     правка под проект — ai-devcontainer skill fork <скилл> [файл]"
echo ""
echo "  2. Открой дашборд (gateway поднимется автоматически) и создай задачу:"
echo "       hermes dashboard"
echo ""
echo "     - Исполнитель: team-lead"
echo "     - Первая строка body НЕОБЯЗАТЕЛЬНО: 'template: doc-only' или 'template: bugfix'"
echo "       (по умолчанию = feature)"
echo ""
echo "     Авто-старт gateway отключается через HERMES_NO_GATEWAY_AUTOSTART=1;"
echo "     поднять gateway отдельно: hermes gateway run"
echo ""
echo "  Перезапускай этот скрипт после любых изменений в .hermes/profiles/,"
echo "  .agents/skills/ или packages/hermes-process/. Он идемпотентный."
echo ""
