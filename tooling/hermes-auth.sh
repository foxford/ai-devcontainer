#!/usr/bin/env bash
# tooling/hermes-auth.sh — одна авторизация Hermes на все проекты машины.
#
# ЗАЧЕМ. Логин Hermes'а лежит в $HERMES_HOME/auth.json, а $HERMES_HOME у нас
# пер-проектный (bind-маунт ~/.ai-devcontainer-dev/<проект>/hermes). Значит
# каждый новый проект встречал человека визардом `hermes setup` с нуля — хотя
# ключ у него давно есть и лежит в соседнем проекте.
#
# КАК. auth.json проекта — симлинк в общий стор на volume platform-ai-tools
# (/opt/ai-tools). Том общий на все проекты и уже примонтирован даже в те, что
# заведены до этой команды, — devcontainer.json править не нужно.
#
# Апстрим такую раскладку поддерживает НАМЕРЕННО: atomic_replace() резолвит
# симлинк перед os.replace, «so the symlink survives» — ровно для случая, когда
# auth.json вынесен из ~/.hermes наружу (utils.py:61-82, GitHub #16743). Все
# записи стора, включая refresh OAuth-токенов, идут через эту функцию, поэтому
# ссылка не отрывается, а обновлённый токен виден всем проектам разом.
#
# Рядом линкуем auth.lock: межпроцессный flock Hermes держит файлом
# auth.lock возле auth.json (auth.py:944). Без общего lock-файла блокировка
# осталась бы внутриконтейнерной, и два проекта могли бы писать стор наперегонки.
#
# Что НЕ шарим: config.yaml и .env. Там проектное — hooks.pre_tool_call с путями
# этого репо, skills.external_dirs, mcp_servers, kanban.*, terminal.* — и общий
# файл растащил бы чужие пути по проектам. Из config.yaml переносим ровно две
# строки (провайдер и модель) и штатной командой `hermes config set`.
#
#   adc hermes status            — где стор, есть ли логин, связан ли проект
#   adc hermes link              — связать проект со стором (идемпотентно)
#   adc hermes save              — отдать свой логин в стор (перекрыть общий)
#   adc hermes unlink            — отвязаться, остаться при своей копии
#   adc hermes configured        — тихая проверка «есть чем ходить в модель» (код возврата)

set -euo pipefail

HERMES_DIR="${HERMES_HOME:-$HOME/.hermes}"
STORE="${AI_DEVCONTAINER_HERMES_STORE:-/opt/ai-tools/share/hermes}"

C_GREEN='\033[0;32m'; C_YELLOW='\033[0;33m'; C_DIM='\033[2m'; C_RESET='\033[0m'
log()  { echo -e "${C_GREEN}==>${C_RESET} $*"; }
warn() { echo -e "${C_YELLOW}!! ${C_RESET}$*" >&2; }
dim()  { echo -e "${C_DIM}$*${C_RESET}"; }

# Файлы, которые уезжают в общий стор. auth.json — данные, auth.lock — только
# межпроцессная блокировка (его содержимое не значит ничего, но общим он быть
# обязан, иначе блокировка не межконтейнерная).
STORE_FILES="auth.json auth.lock"

# Провайдерские ключи, по которым Hermes считает себя настроенным без auth.json.
# Зеркало provider_env_vars из _has_any_provider_configured (main.py:630-745):
# полный реестр там собирается из PROVIDER_REGISTRY, здесь — ходовая выжимка.
# Расхождение не страшно: цена ошибки — лишняя подсказка в логе, а не поломка.
PROVIDER_ENV_VARS="OPENROUTER_API_KEY OPENAI_API_KEY ANTHROPIC_API_KEY ANTHROPIC_TOKEN
GOOGLE_API_KEY GEMINI_API_KEY DEEPSEEK_API_KEY XAI_API_KEY MINIMAX_API_KEY
KIMI_API_KEY KIMI_CODING_API_KEY GLM_API_KEY ZAI_API_KEY NVIDIA_API_KEY
DASHSCOPE_API_KEY GMI_API_KEY ARCEEAI_API_KEY STEPFUN_API_KEY"

# ── общее ─────────────────────────────────────────────────────

# Стор может быть недоступен: команду позвали на хосте, где /opt/ai-tools нет
# вовсе, или том смонтирован read-only. Это не ошибка — просто нечего делать.
store_ready() {
  [ -d "$STORE" ] && return 0
  mkdir -p "$STORE" 2>/dev/null || return 1
  chmod 700 "$STORE" 2>/dev/null || true
}

# Непустой ли auth.json (пустой файл заводит сам Hermes и логином не является).
has_creds() {
  local f="$1"
  [ -s "$f" ] || return 1
  command -v jq >/dev/null 2>&1 || return 0   # без jq довольствуемся размером
  jq -e '(.active_provider // "") != "" or ((.providers // {}) | length) > 0' "$f" >/dev/null 2>&1
}

active_provider() {
  local f="$STORE/auth.json"
  [ -s "$f" ] && command -v jq >/dev/null 2>&1 || return 0
  jq -r '.active_provider // ""' "$f" 2>/dev/null
}

# Ссылка проекта уже смотрит в стор?
linked_to_store() {
  local f="$HERMES_DIR/$1"
  [ -L "$f" ] && [ "$(readlink -f "$f")" = "$(readlink -f "$STORE/$1")" ]
}

# ── link ──────────────────────────────────────────────────────
cmd_link() {
  local quiet="${1:-}"
  if ! store_ready; then
    [ "$quiet" = quiet ] || warn "нет доступа к стору $STORE — авторизация останется пер-проектной"
    return 0
  fi
  mkdir -p "$HERMES_DIR"

  # Первый залогиненный проект наполняет стор: его auth.json переезжает в общий
  # и раздаётся остальным. Если в сторе уже есть логин, а у проекта свой —
  # молча не перетираем НИ ТОТ, НИ ДРУГОЙ: выбор чей оставить за человеком.
  local f
  for f in $STORE_FILES; do
    local mine="$HERMES_DIR/$f" theirs="$STORE/$f"

    if linked_to_store "$f"; then continue; fi

    # Чужой симлинк (человек увёл auth.json в свои дотфайлы) — не наше дело.
    if [ -L "$mine" ]; then
      [ "$quiet" = quiet ] || dim "  $f — уже симлинк мимо стора, не трогаю ($(readlink "$mine"))"
      continue
    fi

    if [ "$f" = "auth.json" ] && [ -e "$mine" ]; then
      if has_creds "$mine" && has_creds "$theirs"; then
        warn "логин есть и в проекте, и в общем сторе — оставляю проектный"
        echo "    отдать свой всем:  adc hermes save" >&2
        echo "    взять общий:       adc hermes unlink && rm ~/.hermes/auth.json && adc hermes link" >&2
        continue
      fi
      if has_creds "$mine"; then
        mv "$mine" "$theirs"
        chmod 600 "$theirs" 2>/dev/null || true
        [ "$quiet" = quiet ] || log "  логин этого проекта уехал в общий стор — теперь он у всех"
      else
        rm -f "$mine"   # пустая заготовка, данных в ней нет
      fi
    elif [ -e "$mine" ]; then
      rm -f "$mine"     # auth.lock — только блокировка, содержимое неважно
    fi

    [ -e "$theirs" ] || { : > "$theirs"; chmod 600 "$theirs" 2>/dev/null || true; }
    ln -sfn "$theirs" "$mine"
  done

  apply_model_hint "$quiet"

  if [ "$quiet" != quiet ]; then
    if has_creds "$STORE/auth.json"; then
      local prov; prov="$(active_provider)"
      log "Проект связан с общим логином Hermes${prov:+ (провайдер: $prov)}"
    else
      log "Проект связан со стором $STORE — логина в нём пока нет"
    fi
  fi
}

# ── model hint ────────────────────────────────────────────────
# Провайдер и модель живут в config.yaml, а он проектный: тащить файл целиком
# нельзя. Переносим две строки — и штатной командой, а не правкой YAML руками.
# Применяем ТОЛЬКО когда у проекта своего выбора ещё нет: осознанно выбранную
# в этом проекте модель не перебиваем.
MODEL_HINT="$STORE/model.env"

apply_model_hint() {
  local quiet="${1:-}"
  [ -f "$MODEL_HINT" ] || return 0
  command -v hermes >/dev/null 2>&1 || return 0
  command -v yq >/dev/null 2>&1 || return 0

  local cfg="$HERMES_DIR/config.yaml" current=""
  [ -f "$cfg" ] && current="$(yq -r '.model.default // .model // ""' "$cfg" 2>/dev/null || true)"
  [ -z "$current" ] || [ "$current" = "null" ] || return 0

  local provider="" model=""
  # shellcheck disable=SC1090
  . "$MODEL_HINT"
  provider="${MODEL_PROVIDER:-}"; model="${MODEL_DEFAULT:-}"
  [ -n "$provider" ] && hermes config set model.provider "$provider" >/dev/null 2>&1 || true
  [ -n "$model" ] && hermes config set model.default "$model" >/dev/null 2>&1 || true
  [ "$quiet" = quiet ] || dim "  провайдер/модель взяты из общего стора: ${provider:-?} / ${model:-?}"
}

save_model_hint() {
  command -v yq >/dev/null 2>&1 || return 0
  local cfg="$HERMES_DIR/config.yaml"
  [ -f "$cfg" ] || return 0
  local provider model
  provider="$(yq -r '.model.provider // ""' "$cfg" 2>/dev/null || true)"
  model="$(yq -r '.model.default // .model // ""' "$cfg" 2>/dev/null || true)"
  [ "$provider" = "null" ] && provider=""
  [ "$model" = "null" ] && model=""
  [ -n "$provider$model" ] || return 0
  printf 'MODEL_PROVIDER=%s\nMODEL_DEFAULT=%s\n' "$provider" "$model" > "$MODEL_HINT"
  dim "  провайдер/модель записаны в стор: ${provider:-?} / ${model:-?}"
}

# ── save ──────────────────────────────────────────────────────
cmd_save() {
  store_ready || { warn "нет доступа к стору $STORE"; exit 1; }
  local mine="$HERMES_DIR/auth.json"

  if linked_to_store auth.json; then
    log "Проект и так пишет прямо в общий стор — сохранять нечего"
    save_model_hint
    return 0
  fi
  has_creds "$mine" || { warn "в $mine нет логина — сначала пройди hermes setup"; exit 1; }

  cp -f "$mine" "$STORE/auth.json"
  chmod 600 "$STORE/auth.json" 2>/dev/null || true
  rm -f "$mine"
  save_model_hint
  cmd_link quiet
  log "Логин этого проекта стал общим: $STORE/auth.json"
  echo "    Новые проекты подхватят его сами на postCreate." >&2
}

# ── unlink ────────────────────────────────────────────────────
cmd_unlink() {
  local f n=0
  for f in $STORE_FILES; do
    linked_to_store "$f" || continue
    rm -f "$HERMES_DIR/$f"
    # auth.json возвращаем копией — иначе проект остался бы вовсе без логина.
    [ "$f" = "auth.json" ] && [ -e "$STORE/$f" ] && {
      cp -f "$STORE/$f" "$HERMES_DIR/$f"; chmod 600 "$HERMES_DIR/$f" 2>/dev/null || true; }
    n=$((n + 1))
  done
  [ "$n" -gt 0 ] && log "Проект отвязан от общего стора, логин остался копией в $HERMES_DIR" \
                 || log "Проект и не был связан со стором"
}

# ── configured ────────────────────────────────────────────────
# Тихая проверка «Hermes есть чем ходить в модель» — по ней post-create решает,
# запускать ли bootstrap. Зеркало _has_any_provider_configured (main.py:630-745)
# в трёх дешёвых проверках: env процесса → $HERMES_HOME/.env → auth.json.
cmd_configured() {
  local var
  for var in $PROVIDER_ENV_VARS; do
    [ -n "${!var:-}" ] && return 0
  done
  if [ -f "$HERMES_DIR/.env" ]; then
    for var in $PROVIDER_ENV_VARS; do
      grep -qE "^[[:space:]]*(export[[:space:]]+)?$var=[[:space:]]*[^[:space:]#]" \
        "$HERMES_DIR/.env" 2>/dev/null && return 0
    done
  fi
  has_creds "$HERMES_DIR/auth.json"
}

# ── status ────────────────────────────────────────────────────
cmd_status() {
  echo "стор:       $STORE$([ -d "$STORE" ] || echo ' (нет — /opt/ai-tools не смонтирован?)')"
  local prov; prov="$(active_provider)"
  if has_creds "$STORE/auth.json"; then
    echo "общий логин: есть${prov:+, активный провайдер: $prov}"
  else
    echo "общий логин: нет — залогинься один раз (hermes setup) в любом проекте"
  fi
  local f
  for f in $STORE_FILES; do
    if linked_to_store "$f"; then
      echo "  $f       → стор (симлинк)"
    elif [ -L "$HERMES_DIR/$f" ]; then
      echo "  $f       → $(readlink "$HERMES_DIR/$f") (чужая ссылка, не трогаем)"
    elif [ -e "$HERMES_DIR/$f" ]; then
      echo "  $f       свой файл проекта (adc hermes save — отдать всем)"
    else
      echo "  $f       нет (adc hermes link — связать)"
    fi
  done
  if cmd_configured; then
    echo "проверка:   Hermes настроен — bootstrap запустится сам на postCreate"
  else
    echo "проверка:   Hermes НЕ настроен, визард ждёт: hermes setup"
    echo "            без TTY, ключом:  hermes auth add <провайдер> --type api-key --api-key <ключ>"
  fi
}

case "${1:-status}" in
  link)       shift; cmd_link "$@";;
  save)       cmd_save;;
  unlink)     cmd_unlink;;
  configured) cmd_configured;;
  status|"")  cmd_status;;
  help|--help|-h) sed -n '/^#   adc hermes/,/configured/p' "$0" | sed 's/^# \{0,1\}//';;
  *) warn "неизвестная команда: $1 (см. adc hermes help)"; exit 1;;
esac
