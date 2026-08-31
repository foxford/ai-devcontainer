#!/usr/bin/env bash
# tooling/wire-agent-skills.sh — раздаёт тех-скиллы на всех агентов проекта.
#
# ДВА СЛОЯ (overlay), приоритет — проектный:
#   1. платформенный  $PLATFORM_ROOT/skills/<skill>/…   (read-only mount, стоковые)
#   2. проектный      <repo>/.agents/skills/<skill>/…   (форки и свои скиллы)
#
# Перекрытие ПОФАЙЛОВОЕ: проект может держать только `SKILL.md`, а `scripts/`
# и `reference/` продолжат ехать с платформы. Это и есть смысл затеи —
# `ai-devcontainer update` меняет стоковую часть во всех проектах сразу, а в гите
# проекта лежит ровно то, чем он отличается.
#
# Результат сборки — НАСТОЯЩИЙ каталог .claude/skills/<skill>/ из симлинков на
# файлы обоих слоёв. Он расходуемый: `.claude/` в .gitignore, пересобирается
# на каждом прогоне. Агенты видят только его.
#
# Перекрыть можно ЛЮБОЙ файл скилла, не только SKILL.md. Отсюда разное
# обращение с двумя видами файлов:
#   • проза (.md/.json/.yaml/…) — СИМЛИНК на слой. Живая: правка в платформе
#     видна проекту сразу, без пересборки.
#   • код (.mjs/.js/.cjs/.ts/.py/.sh) — КОПИЯ в дерево. Иначе Node раскрутил бы
#     симлинк и резолвил `import './lib/x.mjs'` от РЕАЛЬНОГО пути (платформы),
#     молча игнорируя проектный форк соседнего файла. С копиями относительные
#     пути резолвятся внутри собранного дерева и форк любого файла срабатывает.
#     Цена — код обновляется не мгновенно, а на пересборке (postCreate или
#     `ai-devcontainer skill sync`). ~3 МБ на проект, каталог в .gitignore.
#
# Кто и как читает результат:
#   - Claude Code — .claude/skills/<name>/ (проверено грепом бинаря: 54× ".claude/skills")
#   - OpenCode    — тот же .claude/skills (Claude-compat) и .agents/skills (Agent-compat)
#   - Codex       — только $CODEX_HOME/skills (home, не проект) → симлинк на собранное дерево
#   - Hermes      — skills.external_dirs → собранное дерево (см. .hermes/bootstrap.sh)
#   - DSH         — <repo>/.dsh/skills (ранг 100, старше всех проектных) → симлинки
#                   туда же. Формат совпал: <name>/SKILL.md, kebab-case-имя.
#                   .agents/skills он читает и сам (ранг 200), но там лежат ОДНИ
#                   ФОРКИ — платформенный слой без .dsh/skills он бы не увидел.
#
# Индекс скиллов пишется в AGENTS.skills.md (генерируемый, в .gitignore).
# В AGENTS.md — только статичная ссылка на него: иначе каждый `ai-devcontainer update`
# давал бы диф в трекаемом файле проекта, которого человек не делал.
#
# Идемпотентно и без зависимостей (coreutils + awk). Безопасно гонять много раз.

set -euo pipefail

# Скрипт живёт в клоне платформы (/opt/ai-devcontainer/tooling), а НЕ в проекте —
# корень проекта берём из каталога вызова (или $REPO_ROOT), как в setup.sh.
REPO_ROOT="${REPO_ROOT:-$PWD}"
TOOLING_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PLATFORM_ROOT="$(dirname "$TOOLING_DIR")"
cd "$REPO_ROOT"

PLATFORM_SKILLS="${AI_DEVCONTAINER_SKILLS:-$PLATFORM_ROOT/skills}"
PROJECT_SKILLS="$REPO_ROOT/.agents/skills"
CLAUDE_SKILLS="$REPO_ROOT/.claude/skills"
CODEX_SKILLS="${CODEX_HOME:-$HOME/.codex}/skills"
DSH_SKILLS="$REPO_ROOT/.dsh/skills"
INDEX_FILE="$REPO_ROOT/AGENTS.skills.md"
AGENTS_FILE="$REPO_ROOT/AGENTS.md"

# Маркер «этот каталог собрали мы» — чтобы чистка не трогала чужое в .claude/skills.
WIRED_MARK=".ai-devcontainer-wired"

# Маркеры HERMES-SKILLS больше НЕ пишутся: идемпотентность держится на самой
# ссылке (grep по "AGENTS.skills.md"), а не на них. Оставлены только для поиска
# инлайнового списка ПРОШЛОЙ схемы в живущих проектах — его надо вырезать.
# Матчим START по префиксу: текст в скобках исторически менялся, и точное
# сравнение молча дублировало секцию.
MARK_START_PREFIX="<!-- HERMES-SKILLS:START"
MARK_END="<!-- HERMES-SKILLS:END -->"

C_GREEN='\033[0;32m'; C_YELLOW='\033[0;33m'; C_DIM='\033[2m'; C_RESET='\033[0m'
log()  { echo -e "${C_GREEN}==>${C_RESET} $*"; }
warn() { echo -e "${C_YELLOW}!! ${C_RESET}$*" >&2; }
dim()  { echo -e "${C_DIM}$*${C_RESET}"; }

# Вырожденный случай: репозиторий платформы, открытый сам в себе, — слои
# совпадают, работаем одним. realpath на случай симлинка .agents → skeleton/.agents.
same_path() {
  local a b
  a="$(readlink -f "$1" 2>/dev/null || echo "$1")"
  b="$(readlink -f "$2" 2>/dev/null || echo "$2")"
  [ "$a" = "$b" ]
}

[ -d "$PLATFORM_SKILLS" ] || warn "нет $PLATFORM_SKILLS — платформенный слой пуст (клон платформы не примонтирован?)"
if same_path "$PLATFORM_SKILLS" "$PROJECT_SKILLS"; then
  dim "  слои совпадают ($PLATFORM_SKILLS) — работаю одним"
  PROJECT_SKILLS=""
fi

# ── собрать имена скиллов из обоих слоёв ─────────────────────
# Скилл = каталог первого уровня. Имя уникально: проектный слой ПЕРЕКРЫВАЕТ
# платформенный с тем же именем, а не соседствует с ним.
declare -A HAS_PLATFORM=() HAS_PROJECT=()
SKILL_NAMES=()

collect_layer() {
  local root="$1" kind="$2" d name
  [ -n "$root" ] && [ -d "$root" ] || return 0
  for d in "$root"/*/; do
    [ -d "$d" ] || continue
    name="$(basename "$d")"
    case "$name" in .*) continue;; esac      # служебные dot-каталоги пропускаем
    if [ "$kind" = platform ]; then
      # платформенный слой обязан нести SKILL.md — иначе это не скилл
      [ -f "$d/SKILL.md" ] || continue
      HAS_PLATFORM[$name]=1
    else
      HAS_PROJECT[$name]=1
    fi
  done
}
collect_layer "$PLATFORM_SKILLS" platform
collect_layer "$PROJECT_SKILLS" project

for name in "${!HAS_PLATFORM[@]}" "${!HAS_PROJECT[@]}"; do
  case " ${SKILL_NAMES[*]:-} " in *" $name "*) continue;; esac
  SKILL_NAMES+=("$name")
done
if [ "${#SKILL_NAMES[@]}" -eq 0 ]; then
  warn "ни одного скилла не найдено (платформа: $PLATFORM_SKILLS, проект: ${PROJECT_SKILLS:-—}) — выходим"
  exit 0
fi
IFS=$'\n' SKILL_NAMES=($(printf '%s\n' "${SKILL_NAMES[@]}" | sort)); unset IFS

# ── 1. Сборка overlay-дерева в .claude/skills/<name>/ ────────
mkdir -p "$CLAUDE_SKILLS"

# Чистка того, что собирали мы: устаревшие имена + легаси-симлинки прошлой
# схемы (.claude/skills/<name> → ../../.agents/skills/<name>). Чужие настоящие
# каталоги и симлинки в другие репо не трогаем.
if [ -d "$CLAUDE_SKILLS" ]; then
  for entry in "$CLAUDE_SKILLS"/*; do
    [ -e "$entry" ] || [ -L "$entry" ] || continue
    name="$(basename "$entry")"
    known=""
    case " ${SKILL_NAMES[*]} " in *" $name "*) known=1;; esac
    if [ -L "$entry" ]; then
      target="$(readlink "$entry")"
      case "$target" in
        *".agents/skills"*) rm -f "$entry"; dim "  - снял легаси-симлинк $name";;
      esac
      continue
    fi
    if [ -f "$entry/$WIRED_MARK" ] && [ -z "$known" ]; then
      rm -rf "$entry"; dim "  - убрал устаревший скилл $name"
    fi
  done
fi

# place_file <src> <dst> — код кладём копией, прозу симлинком (см. шапку).
# Симлинк абсолютным путём: слои лежат на разных mount-корнях
# (/opt/ai-devcontainer и /workspaces/<repo>), относительный был бы хрупким.
place_file() {
  mkdir -p "$(dirname "$2")"
  case "$1" in
    *.mjs|*.js|*.cjs|*.ts|*.tsx|*.py|*.sh) cp -p "$1" "$2" ;;
    *) ln -sfn "$1" "$2" ;;
  esac
}

declare -A FORKED_COUNT=()
built=0
for name in "${SKILL_NAMES[@]}"; do
  plat_dir="$PLATFORM_SKILLS/$name"
  proj_dir="${PROJECT_SKILLS:+$PROJECT_SKILLS/$name}"
  dest="$CLAUDE_SKILLS/$name"

  # Дерево расходуемое — пересобираем с нуля, чтобы не оставалось файлов,
  # удалённых на любом из слоёв.
  rm -rf "$dest"; mkdir -p "$dest"
  : > "$dest/$WIRED_MARK"

  forked=0
  # платформенный слой: файл берём, если проект его не перекрыл
  if [ -n "${HAS_PLATFORM[$name]:-}" ]; then
    while IFS= read -r rel; do
      if [ -n "$proj_dir" ] && [ -f "$proj_dir/$rel" ]; then
        place_file "$proj_dir/$rel" "$dest/$rel"; forked=$((forked + 1))
      else
        place_file "$plat_dir/$rel" "$dest/$rel"
      fi
    done < <(cd "$plat_dir" && find . -type f -printf '%P\n' | sort)
  fi
  # проектный слой: файлы, которых на платформе нет вовсе
  if [ -n "${HAS_PROJECT[$name]:-}" ] && [ -n "$proj_dir" ] && [ -d "$proj_dir" ]; then
    while IFS= read -r rel; do
      [ -e "$dest/$rel" ] && continue
      place_file "$proj_dir/$rel" "$dest/$rel"
      [ -n "${HAS_PLATFORM[$name]:-}" ] && forked=$((forked + 1))
    done < <(cd "$proj_dir" && find . -type f -printf '%P\n' | sort)
  fi

  if [ ! -e "$dest/SKILL.md" ]; then
    warn "скилл '$name' без SKILL.md ни на одном слое — пропускаю"
    rm -rf "$dest"
    continue
  fi
  FORKED_COUNT[$name]=$forked
  built=$((built + 1))
done

# Пересобрать список, выкинув пропущенные (без SKILL.md)
LIVE_NAMES=()
for name in "${SKILL_NAMES[@]}"; do
  [ -d "$CLAUDE_SKILLS/$name" ] && LIVE_NAMES+=("$name")
done
SKILL_NAMES=("${LIVE_NAMES[@]}")

log "Собрано скиллов: $built (платформа: ${#HAS_PLATFORM[@]}, проектных перекрытий: ${#HAS_PROJECT[@]})"

# ── 2. Codex: симлинки в $CODEX_HOME/skills на собранное дерево ─
# Codex ищет скиллы только в home. Глоб /* пропускает dot-каталоги
# (.system/.curated/…), так что системные скиллы Codex не трогаем.
mkdir -p "$CODEX_SKILLS"
for entry in "$CODEX_SKILLS"/*; do
  [ -L "$entry" ] || continue
  target="$(readlink "$entry")"
  case "$target" in
    "$REPO_ROOT/.claude/skills/"*|"$REPO_ROOT/.agents/skills/"*) ;;  # наш — ревизуем
    *) continue ;;                                                    # чужой — не трогаем
  esac
  name="$(basename "$entry")"
  known=""
  case " ${SKILL_NAMES[*]} " in *" $name "*) known=1;; esac
  if [ -z "$known" ] || [ ! -e "$entry" ]; then
    rm -f "$entry"; dim "  - убрал устаревший симлинк $CODEX_SKILLS/$name"
  fi
done
for name in "${SKILL_NAMES[@]}"; do
  ln -sfn "$CLAUDE_SKILLS/$name" "$CODEX_SKILLS/$name"
done
log "Codex: ${#SKILL_NAMES[@]} симлинк(ов) в $CODEX_SKILLS/ (нужен рестарт codex для подхвата)"

# ── 2b. DSH: симлинки в <repo>/.dsh/skills на собранное дерево ─
# У DSH шесть роутов скиллов с рангами; проектных два — .dsh/skills (100) и
# .agents/skills (200). Второй он читает сам, но там по замыслу лежат только
# ОТЛИЧИЯ проекта: без .dsh/skills агент увидел бы форки и ни одного стокового
# скилла. Поэтому кладём сюда собранное дерево целиком — оно уже с учётом
# форков, и старший ранг снимает вопрос о дублях с .agents/skills.
#
# Каталог, а не симлинк на корень: рекурсивный обход `**/SKILL.md` у DSH не
# поддержан, скиллы он ищет ровно на первом уровне, а корень-симлинк watcher
# может и не отследить. Пер-скилловые ссылки — та же схема, что у Codex.
mkdir -p "$DSH_SKILLS"
for entry in "$DSH_SKILLS"/*; do
  [ -L "$entry" ] || continue
  target="$(readlink "$entry")"
  case "$target" in
    "$CLAUDE_SKILLS/"*) ;;   # наш — ревизуем
    *) continue ;;            # чужой — не трогаем
  esac
  name="$(basename "$entry")"
  known=""
  case " ${SKILL_NAMES[*]} " in *" $name "*) known=1;; esac
  if [ -z "$known" ] || [ ! -e "$entry" ]; then
    rm -f "$entry"; dim "  - убрал устаревший симлинк $DSH_SKILLS/$name"
  fi
done
# Имя скилла у DSH обязано быть kebab-case (^[a-z0-9]+(-[a-z0-9]+)*$), иначе он
# молча пропустит каталог. Наши стоковые ему соответствуют, а проектный скилл
# может назваться как угодно — предупреждаем, а не гадаем.
dsh_n=0
for name in "${SKILL_NAMES[@]}"; do
  case "$name" in
    *[!a-z0-9-]*|-*|*-|*--*)
      warn "DSH: скилл '$name' не kebab-case — он его не увидит (переименуй)"
      continue ;;
  esac
  ln -sfn "$CLAUDE_SKILLS/$name" "$DSH_SKILLS/$name"
  dsh_n=$((dsh_n + 1))
done
log "DSH: $dsh_n симлинк(ов) в $DSH_SKILLS/ (ранг 100; нужен рестарт dsh)"

# .dsh/ — генерируемый каталог (симлинки на .claude/skills, которое само в
# .gitignore). Разовая правка, как ссылка на индекс в AGENTS.md ниже.
GITIGNORE="$REPO_ROOT/.gitignore"
if [ -f "$GITIGNORE" ] && ! grep -qF "/.dsh/" "$GITIGNORE"; then
  printf '\n# DSH (DeepSeek Harness): роут скиллов ранга 100 — симлинки на собранное\n# дерево .claude/skills, раздаёт tooling/wire-agent-skills.sh. Генерируемое.\n/.dsh/\n' >> "$GITIGNORE"
  log ".gitignore: добавил /.dsh/ (разовая правка)"
fi

# ── 3. Индекс в AGENTS.skills.md (генерируемый, вне гита) ────
extract_field() {
  # extract_field <file> <field> — значение из YAML-фронтматтера (первый блок ---).
  awk -v field="$2" '
    NR==1 && $0=="---" { inb=1; next }
    inb && $0=="---"   { exit }
    inb && $0 ~ "^"field":" {
      sub("^"field":[[:space:]]*", "")
      gsub(/^"|"$/, "")
      print
      exit
    }
  ' "$1"
}

{
  printf '<!-- Генерируется tooling/wire-agent-skills.sh. Не редактировать, не коммитить. -->\n\n'
  printf '# Тех-скиллы проекта\n\n'
  printf 'Собрано из двух слоёв, приоритет у проектного:\n\n'
  printf -- '- платформенный — `%s` (read-only, общий для всех проектов)\n' "$PLATFORM_SKILLS"
  printf -- '- проектный — `.agents/skills/` (только отличия: форки файлов и свои скиллы)\n\n'
  printf 'Читают это агенты из `.claude/skills/` — собранного дерева (в .gitignore,\n'
  printf 'пересобирается): Claude и OpenCode напрямую, Codex через симлинки в\n'
  printf '`~/.codex/skills/`, Hermes через `skills.external_dirs`, DSH через\n'
  printf 'симлинки в `.dsh/skills/` (его роут ранга 100).\n\n'
  printf '**Править скилл под проект — только через `ai-devcontainer skill`, не копированием:**\n'
  printf 'копия перекроет платформенный слой целиком и проект перестанет получать\n'
  printf 'обновления по этому скиллу.\n\n'
  printf '```bash\n'
  printf 'ai-devcontainer skill fork <скилл> [файл…]   # взять под правку (по умолчанию SKILL.md)\n'
  printf 'ai-devcontainer skill status                 # разошлась ли платформа под форками\n'
  printf 'ai-devcontainer skill unfork <скилл>         # вернуться на платформенную версию\n'
  printf 'ai-devcontainer skill sync                   # пересобрать после ai-devcontainer update\n'
  printf '```\n\n'
  printf 'Перекрыть можно любой файл, не только `SKILL.md`. Форк фиксирует базовую\n'
  printf 'версию в `.agents/skills-base/` — по ней `status` видит, что апстрим уехал.\n'
  printf 'Свой скилл проекта — просто каталог с `SKILL.md` в `.agents/skills/`.\n\n'
  printf 'Пометка **(форк)** — проект перекрывает часть файлов платформенного скилла.\n\n'
  for name in "${SKILL_NAMES[@]}"; do
    desc="$(extract_field "$CLAUDE_SKILLS/$name/SKILL.md" description)"
    mark=""
    if [ -z "${HAS_PLATFORM[$name]:-}" ]; then
      mark=" _(проектный)_"
    elif [ "${FORKED_COUNT[$name]:-0}" -gt 0 ]; then
      mark=" _(форк: ${FORKED_COUNT[$name]} файл(ов))_"
    fi
    printf -- '- **%s**%s — %s\n' "$name" "$mark" "${desc:-—}"
  done
} > "$INDEX_FILE"
log "Индекс: $INDEX_FILE"

# ── 4. Платформенные доки (AGENTS.platform.md, MONOREPO.md, …) ──
# Тот же принцип: общее раздаёт платформа, проект дополняет своим файлом.
# Идёт ДО работы с AGENTS.md: если платформенный док разложен, ссылку на индекс
# несёт он, и трекаемый AGENTS.md проекта трогать не нужно вовсе.
if [ -f "$TOOLING_DIR/wire-docs.sh" ]; then
  REPO_ROOT="$REPO_ROOT" bash "$TOOLING_DIR/wire-docs.sh" || warn "wire-docs.sh failed (не блокирует)"
fi

# ── 5. AGENTS.md проекта: ссылка на индекс, если её некому нести ──
POINTER="Полный список скиллов с описаниями — в [AGENTS.skills.md](AGENTS.skills.md) (генерируется, в .gitignore). Прочитай его, если подбираешь скилл под задачу."

# Ссылку на индекс несёт AGENTS.platform.md — тогда в AGENTS.md проекта лезть
# незачем. Остаётся одно дело: вычистить инлайновый список ПРОШЛОЙ схемы, если
# он там застрял, — он давно врёт.
HAS_PLATFORM_DOC=0
[ -e "$REPO_ROOT/AGENTS.platform.md" ] && HAS_PLATFORM_DOC=1

if [ ! -f "$AGENTS_FILE" ]; then
  dim "  нет $AGENTS_FILE — ссылку на индекс не ставлю"
elif grep -qF "AGENTS.skills.md" "$AGENTS_FILE" && ! grep -qF "$MARK_START_PREFIX" "$AGENTS_FILE"; then
  dim "  AGENTS.md трогать не нужно"
elif [ "$HAS_PLATFORM_DOC" = 1 ] && ! grep -qF "$MARK_START_PREFIX" "$AGENTS_FILE"; then
  dim "  ссылку на индекс несёт AGENTS.platform.md — AGENTS.md не трогаю"
else
  TMP="$(mktemp)"
  if grep -qF "$MARK_START_PREFIX" "$AGENTS_FILE"; then
    # была инлайновая секция прошлой схемы — схлопываем её в голую ссылку,
    # маркеры при этом уходят: держать их больше не за чем
    [ "$HAS_PLATFORM_DOC" = 1 ] && POINTER=""
    awk -v block="$POINTER" \
        -v s="$MARK_START_PREFIX" -v e="$MARK_END" '
      index($0, s) && !started { print block; started=1; skip=1; next }
      index($0, s) { next }
      index($0, e) { skip=0; next }
      skip { next }
      { print }
    ' "$AGENTS_FILE" > "$TMP"
  else
    { cat "$AGENTS_FILE"; printf '\n## Тех-скиллы\n\n%s\n' "$POINTER"; } > "$TMP"
  fi
  mv "$TMP" "$AGENTS_FILE"
  log "AGENTS.md: поставил ссылку на индекс (разовая правка, дальше файл не трогаю)"
fi

log "Готово. Слои: платформа $PLATFORM_SKILLS + проект ${PROJECT_SKILLS:-—} → $CLAUDE_SKILLS → Codex + DSH"
