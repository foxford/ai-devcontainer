#!/usr/bin/env bash
# tooling/skill.sh — реализация `adc skill`. Напрямую не зовут:
# точка входа одна и та же на хосте и в контейнере — `adc skill <cmd>`.
#
#   adc skill list             — что откуда приезжает
#   adc skill status           — форки и разошлась ли под ними платформа
#   adc skill fork <skill> [файл…]  — взять файл(ы) платформы под правку
#   adc skill unfork <skill> [файл] — вернуться на платформенную версию
#   adc skill migrate          — разово: выкинуть вендоренные копии
#   adc skill sync             — пересобрать .claude/skills после update
#
# Два слоя (см. tooling/wire-agent-skills.sh):
#   платформа  $PLATFORM_ROOT/skills/<skill>/…      — общее, обновляется централизованно
#   проект     <repo>/.agents/skills/<skill>/…      — только то, чем проект отличается
#
# База форка (платформенная версия НА МОМЕНТ форка) лежит копией в
# <repo>/.agents/skills-base/<skill>/…. Она нужна ровно для одного вопроса:
# «платформа уехала с тех пор, как я форкнул?» — отвечается через cmp, без
# реестров и без зависимости от jq/python.

set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$PWD}"
while [ "$REPO_ROOT" != "/" ] && [ ! -d "$REPO_ROOT/.git" ] && [ ! -d "$REPO_ROOT/.agents" ]; do
  REPO_ROOT="$(dirname "$REPO_ROOT")"
done
[ "$REPO_ROOT" = "/" ] && REPO_ROOT="$PWD"

PLATFORM_ROOT="${AI_DEVCONTAINER_HOME:-/opt/ai-devcontainer}"
[ -d "$PLATFORM_ROOT/skills" ] || PLATFORM_ROOT="$HOME/.local/share/ai-devcontainer"
PLATFORM_SKILLS="${AI_DEVCONTAINER_SKILLS:-$PLATFORM_ROOT/skills}"

PROJECT_SKILLS="$REPO_ROOT/.agents/skills"
BASE_DIR="$REPO_ROOT/.agents/skills-base"

C_GREEN='\033[0;32m'; C_YELLOW='\033[0;33m'; C_RED='\033[0;31m'; C_DIM='\033[2m'; C_RESET='\033[0m'
log()  { echo -e "${C_GREEN}==>${C_RESET} $*"; }
warn() { echo -e "${C_YELLOW}!! ${C_RESET}$*" >&2; }
err()  { echo -e "${C_RED}xx ${C_RESET}$*" >&2; }
dim()  { echo -e "${C_DIM}$*${C_RESET}"; }

[ -d "$PLATFORM_SKILLS" ] || { err "не нахожу платформенные скиллы ($PLATFORM_SKILLS). Внутри контейнера это /opt/ai-devcontainer/skills — примонтирован ли клон платформы?"; exit 1; }

rel_files() { (cd "$1" && find . -type f -printf '%P\n' | sort); }

# Генерируемое и раздаваемое платформой в гите проекта не нужно. .gitignore —
# файл проекта, поэтому правки в скелете сюда не доезжают: дописываем сами.
# ignore_block <заголовок> <паттерн…> — дописать недостающие строки одним
# блоком под общим комментарием, а не повторять комментарий на каждой.
ignore_block() {
  local gi="$REPO_ROOT/.gitignore" why="$1"; shift
  [ -f "$gi" ] || return 0
  local pat missing=()
  for pat in "$@"; do
    grep -qxF "$pat" "$gi" || missing+=("$pat")
  done
  [ "${#missing[@]}" -gt 0 ] || return 0
  { printf '\n# %s\n' "$why"; printf '%s\n' "${missing[@]}"; } >> "$gi"
  log "  .gitignore: добавил ${missing[*]}"
}

ensure_gitignore() {
  ignore_block "Индекс скиллов — генерируется wire-agent-skills.sh из двух слоёв" \
               "AGENTS.skills.md"
  # Доки платформы игнорим только те, что реально раздаются симлинком: если
  # проект забрал док себе (настоящий файл), он трекается и правило было бы ложью.
  local dst to docs=()
  for dst in "AGENTS.platform.md" "MONOREPO.md" ".agents/skills/README.md" \
             "plans/README.md" ".agents/mcp.secrets.env.example"; do
    to="$REPO_ROOT/$dst"
    [ -e "$to" ] && [ ! -L "$to" ] && continue
    docs+=("/$dst")
  done
  [ "${#docs[@]}" -gt 0 ] && ignore_block "Доки платформы — раздаются симлинками (tooling/wire-docs.sh)" "${docs[@]}"
  return 0
}

# .hermes/bootstrap.sh — копия, живущая в проекте; платформа его не обновляет,
# и проект вправе его кастомизировать. Но после миграции .agents/skills содержит
# только форки, а Hermes зарегистрирован именно на него — то есть остаётся без
# скиллов, причём молча. Поэтому патчим ТОЧЕЧНО: две известные строки старой
# схемы, а не файл целиком. Чужие правки при этом уцелеют; если формы строк не
# узнаём — не трогаем и говорим об этом.
patch_hermes() {
  local bs="$REPO_ROOT/.hermes/bootstrap.sh"
  [ -f "$bs" ] || return 0
  if grep -q '\.claude/skills' "$bs"; then return 0; fi

  if ! grep -qF 'SKILLS_PATH="$PROJECT_ROOT/.agents/skills"' "$bs"; then
    warn ".hermes/bootstrap.sh регистрирует Hermes на .agents/skills, но строки незнакомой формы —"
    echo "    патчить вслепую не буду. Эталон: $PLATFORM_ROOT/skeleton/.hermes/bootstrap.sh" >&2
    return 0
  fi

  cp -p "$bs" "$bs.bak"
  # 1) сам источник для skills.external_dirs
  sed -i 's#^SKILLS_PATH="\$PROJECT_ROOT/\.agents/skills"$#SKILLS_PATH="$PROJECT_ROOT/.claude/skills"\n[ -d "$SKILLS_PATH" ] || SKILLS_PATH="$PROJECT_ROOT/.agents/skills"#' "$bs"
  # 2) вселенная скиллов для скоупинга профилей — там путь захардкожен литералом
  #    (вызов идёт РАНЬШЕ присваивания выше, поэтому $SKILLS_PATH тут нельзя)
  sed -i 's#"\$PROJECT_ROOT/\.agents/skills" "\$PROJECT_ROOT/\.hermes/skills"#"$PROJECT_ROOT/.claude/skills" "$PROJECT_ROOT/.hermes/skills"#' "$bs"

  if bash -n "$bs" 2>/dev/null; then
    log "  пропатчил .hermes/bootstrap.sh на .claude/skills (бэкап: bootstrap.sh.bak)"
    echo "    перезапусти: bash .hermes/bootstrap.sh" >&2
  else
    mv -f "$bs.bak" "$bs"
    warn "патч .hermes/bootstrap.sh сломал бы синтаксис — откатил, поправь руками"
  fi
}

# Доки, которые теперь раздаёт платформа (см. tooling/wire-docs.sh). В проекте
# они лежат трекаемыми копиями со времён `adc new` — их надо убрать,
# иначе копия перекроет платформенную и общее правило снова перестанет доезжать.
# Убираем ТОЛЬКО то, что совпадает с платформенным байт в байт; разошедшееся
# оставляем проекту и говорим, где посмотреть диф.
migrate_docs() {
  local docs_src="$PLATFORM_ROOT/docs" pair src dst from to
  [ -d "$docs_src" ] || return 0
  for pair in "AGENTS.platform.md:AGENTS.platform.md" \
              "MONOREPO.md:MONOREPO.md" \
              "skills-README.md:.agents/skills/README.md" \
              "plans-README.md:plans/README.md" \
              "mcp-secrets.env.example:.agents/mcp.secrets.env.example"; do
    src="${pair%%:*}"; dst="${pair#*:}"
    from="$docs_src/$src"; to="$REPO_ROOT/$dst"
    [ -f "$from" ] || continue
    [ -e "$to" ] && [ ! -L "$to" ] || continue
    if cmp -s "$from" "$to"; then
      rm -f "$to"
      log "  $dst — копия платформенной, убрал (дальше едет из платформы)"
    else
      warn "$dst отличается от платформенного — оставил проекту"
      echo "    посмотреть: diff '$from' '$to'" >&2
    fi
  done

  # Разложить док мало — на него должен кто-то ссылаться, иначе агенты его не
  # прочитают. Ссылаются два файла проекта: CLAUDE.md (через @-импорт, нативно
  # для Claude Code) и первые строки AGENTS.md (его читают Codex, OpenCode,
  # Hermes). В новых проектах это есть из скелета; живым — дописываем здесь.
  local cm="$REPO_ROOT/CLAUDE.md"
  if [ -f "$cm" ] && ! grep -q 'AGENTS.platform.md' "$cm"; then
    # через cat, а не $(cat) — подстановка съедает перевод строки в конце
    { printf '@AGENTS.platform.md\n'; cat "$cm"; } > "$cm.tmp" && mv "$cm.tmp" "$cm"
    log "  CLAUDE.md: добавил импорт @AGENTS.platform.md"
  fi

  local am="$REPO_ROOT/AGENTS.md"
  if [ -f "$am" ] && ! grep -q 'AGENTS.platform.md' "$am"; then
    # вставляем сразу после заголовка H1, чтобы ссылка попалась агенту первой
    awk 'NR==1 { print; print ""; \
      print "**Сначала прочитай [AGENTS.platform.md](AGENTS.platform.md)** — общий контракт"; \
      print "платформы (quality gates, жёсткие ограничения, Hermes-флоу, скиллы). Он"; \
      print "раздаётся платформой и обновляется централизованно; здесь — проектное."; \
      next } { print }' "$am" > "$am.tmp" && mv "$am.tmp" "$am"
    log "  AGENTS.md: добавил ссылку на платформенный контракт"
  fi

  # Переписать AGENTS.md целиком нельзя — там вперемешку общее и проектное.
  if [ -f "$am" ] && grep -q '^## Quality gates' "$am"; then
    warn "AGENTS.md проекта всё ещё содержит платформенные секции (Quality gates и т.п.) —"
    echo "    они продублированы в AGENTS.platform.md. Оставь в AGENTS.md только проектное;" >&2
    echo "    образец: $PLATFORM_ROOT/skeleton/AGENTS.md" >&2
  fi
}

cmd_list() {
  echo "платформа: $PLATFORM_SKILLS"
  echo "проект:    $PROJECT_SKILLS"
  echo
  local name plat proj
  for d in "$PLATFORM_SKILLS"/*/; do
    [ -d "$d" ] || continue
    name="$(basename "$d")"
    if [ -d "$PROJECT_SKILLS/$name" ]; then
      printf '  %-28s платформа + форк (%s файл(ов))\n' "$name" "$(rel_files "$PROJECT_SKILLS/$name" | wc -l)"
    else
      printf '  %-28s платформа\n' "$name"
    fi
  done
  for d in "$PROJECT_SKILLS"/*/; do
    [ -d "$d" ] || continue
    name="$(basename "$d")"
    [ -d "$PLATFORM_SKILLS/$name" ] && continue
    printf '  %-28s только проект\n' "$name"
  done
}

cmd_status() {
  local any=0 name rel base plat proj
  for d in "$PROJECT_SKILLS"/*/; do
    [ -d "$d" ] || continue
    name="$(basename "$d")"
    while IFS= read -r rel; do
      any=1
      proj="$PROJECT_SKILLS/$name/$rel"
      plat="$PLATFORM_SKILLS/$name/$rel"
      base="$BASE_DIR/$name/$rel"
      if [ ! -e "$plat" ]; then
        printf '  %-40s %s\n' "$name/$rel" "своё (на платформе нет)"
      elif [ ! -e "$base" ]; then
        printf '  %-40s %s\n' "$name/$rel" "форк без базы — сверить вручную: diff '$plat' '$proj'"
      elif cmp -s "$base" "$plat"; then
        printf '  %-40s %s\n' "$name/$rel" "форк актуален"
      else
        printf '  %-40s %b\n' "$name/$rel" "${C_YELLOW}платформа уехала${C_RESET} — diff '$base' '$plat'"
      fi
    done < <(rel_files "$PROJECT_SKILLS/$name" 2>/dev/null || true)
  done
  [ "$any" = 1 ] || log "форков нет — всё едет с платформы"
}

cmd_fork() {
  local name="${1:?Использование: adc skill fork <skill> [файл…]}"; shift || true
  local src="$PLATFORM_SKILLS/$name"
  [ -d "$src" ] || { err "нет платформенного скилла '$name' (см. adc skill list)"; exit 1; }

  local files=("$@")
  if [ "${#files[@]}" -eq 0 ]; then
    files=(SKILL.md)
    dim "  файл не указан — форкаю SKILL.md (остальное продолжит ехать с платформы)"
  fi

  local rel
  for rel in "${files[@]}"; do
    [ -f "$src/$rel" ] || { err "нет файла $name/$rel на платформе"; exit 1; }
    if [ -e "$PROJECT_SKILLS/$name/$rel" ]; then
      warn "$name/$rel уже форкнут — пропускаю"; continue
    fi
    mkdir -p "$(dirname "$PROJECT_SKILLS/$name/$rel")" "$(dirname "$BASE_DIR/$name/$rel")"
    cp -p "$src/$rel" "$PROJECT_SKILLS/$name/$rel"
    cp -p "$src/$rel" "$BASE_DIR/$name/$rel"
    log "форк: .agents/skills/$name/$rel"
  done

  case " ${files[*]} " in
    *.mjs\ *|*.js\ *|*.ts\ *|*.py\ *|*.sh\ *)
      dim "  форкнут код: собранное дерево держит код копиями, так что относительные импорты увидят именно твою версию" ;;
  esac
  cmd_sync
}

cmd_unfork() {
  local name="${1:?Использование: adc skill unfork <skill> [файл]}"; shift || true
  local rel="${1:-}"
  if [ -n "$rel" ]; then
    rm -f "$PROJECT_SKILLS/$name/$rel" "$BASE_DIR/$name/$rel"
    log "вернул на платформенную версию: $name/$rel"
  else
    rm -rf "$PROJECT_SKILLS/$name" "$BASE_DIR/$name"
    log "вернул на платформенную версию весь скилл: $name"
  fi
  find "$PROJECT_SKILLS" "$BASE_DIR" -type d -empty -delete 2>/dev/null || true
  cmd_sync
}

# Разовая миграция проекта, у которого скиллы вендорены целиком (схема до overlay):
# файл байт-в-байт как на платформе — выкидываем (поедет с платформы), отличается —
# оставляем форком и фиксируем базу.
cmd_migrate() {
  [ -d "$PROJECT_SKILLS" ] || { log "нет $PROJECT_SKILLS — мигрировать нечего"; return 0; }
  local dropped=0 kept=0 own=0 name rel proj plat
  for d in "$PROJECT_SKILLS"/*/; do
    [ -d "$d" ] || continue
    name="$(basename "$d")"
    if [ ! -d "$PLATFORM_SKILLS/$name" ]; then
      own=$((own + 1)); dim "  свой скилл, не трогаю: $name"; continue
    fi
    while IFS= read -r rel; do
      proj="$PROJECT_SKILLS/$name/$rel"
      plat="$PLATFORM_SKILLS/$name/$rel"
      if [ -f "$plat" ] && cmp -s "$proj" "$plat"; then
        rm -f "$proj"; dropped=$((dropped + 1))
      else
        kept=$((kept + 1))
        if [ -f "$plat" ]; then
          mkdir -p "$(dirname "$BASE_DIR/$name/$rel")"
          # базы нет и взяться ей неоткуда — фиксируем ТЕКУЩУЮ платформенную.
          # Это значит «сверено сейчас», а не «форкнуто отсюда»: расхождение
          # придётся посмотреть глазами один раз.
          cp -p "$plat" "$BASE_DIR/$name/$rel"
          printf '  оставляю форк: %s/%s\n' "$name" "$rel"
        fi
      fi
    done < <(rel_files "$PROJECT_SKILLS/$name")
  done
  find "$PROJECT_SKILLS" -type d -empty -delete 2>/dev/null || true
  mkdir -p "$PROJECT_SKILLS"
  migrate_docs
  log "миграция: выкинуто копий $dropped, оставлено форков $kept, своих скиллов $own"
  ensure_gitignore
  patch_hermes
  [ "$kept" -gt 0 ] && dim "  проверь форки: adc skill status"
  cmd_sync
}

cmd_sync() {
  local wire="$PLATFORM_ROOT/tooling/wire-agent-skills.sh"
  [ -f "$wire" ] || { err "не нахожу $wire"; exit 1; }
  REPO_ROOT="$REPO_ROOT" bash "$wire"
  # Сборка кладёт в проект генерируемое (индекс скиллов) и раздаваемое
  # (симлинки доков) — им место в .gitignore, иначе они висят в git status, а
  # симлинк на абсолютный путь клона платформы однажды окажется закоммичен.
  # Раньше это делала только migrate — и каждый НОВЫЙ раздаваемый док
  # приходилось доигнорировать в проектах руками. Диф в трекаемом .gitignore
  # тут разовый (дописываются лишь недостающие строки), а не на каждый прогон.
  ensure_gitignore
}

case "${1:-help}" in
  list)    cmd_list;;
  status)  cmd_status;;
  fork)    shift; cmd_fork "$@";;
  unfork)  shift; cmd_unfork "$@";;
  migrate) cmd_migrate;;
  sync)    cmd_sync;;
  help|--help|-h) sed -n '4,11p' "$0" | sed 's/^# \{0,1\}//';;
  *) err "Неизвестная команда: $1 (см. adc skill help)"; exit 1;;
esac
