#!/usr/bin/env bash
# tooling/mcp.sh — MCP-серверы глазами человека: посмотреть, добавить, включить.
#
# Разделение обязанностей ровно как у скиллов (skill.sh ↔ wire-agent-skills.sh):
# здесь — команды, которые набирает человек; в wire-mcp.sh — движок, который
# сливает слои и раскладывает результат по четырём агентам.
#
# ПОЧЕМУ ОТДЕЛЬНЫЙ ФАЙЛ, А НЕ ФЛАГИ В ДВИЖКЕ. Движок зовётся из postCreate и
# обязан оставаться неинтерактивным и молчаливым. Команды человека — наоборот,
# спрашивают и печатают. Смешать это в одном скрипте значит рано или поздно
# подвесить сборку контейнера на промпте «а в какой слой положить?».
#
# ОДИН ИСТОЧНИК ПРАВДЫ. `list` не считает слои сам — он берёт готовый план у
# движка (`wire-mcp.sh --dump-plan`) и только рисует его. Иначе неизбежно
# наступает день, когда list показывает одно, а агент получает другое; такая
# картинка хуже, чем её отсутствие, потому что ей верят.
#
# ЧЕТЫРЕ СЛОЯ, снизу вверх. Правило приоритета одно и читается фразой:
# ПРОЕКТНОЕ БЬЁТ ГЛОБАЛЬНОЕ, МОЁ БЬЁТ ОБЩЕЕ.
#
#   глобальный (global)   платформа, приезжает с devcontainer'ом   только чтение
#   пользовательский (user)  мои серверы во ВСЕХ моих проектах     /opt/ai-tools
#   проектный (project)   серверы команды, лежат В ГИТЕ            .agents/mcp.json
#   локальный (local)     мои серверы только в этом проекте        .agents/mcp.local.json
#
# Имена слоёв взяты у `claude mcp add --scope`, а не придуманы свои: человек,
# знающий любого из четырёх агентов, читает вывод без перевода.

set -euo pipefail

TOOLING_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PLATFORM_ROOT="$(dirname "$TOOLING_DIR")"
REPO_ROOT="${REPO_ROOT:-$PWD}"
WIRE="$TOOLING_DIR/wire-mcp.sh"

USER_STORE="${AI_DEVCONTAINER_MCP_STORE:-/opt/ai-tools/share/mcp}"

C_GREEN='\033[0;32m'; C_YELLOW='\033[0;33m'; C_RED='\033[0;31m'
C_DIM='\033[2m'; C_BOLD='\033[1m'; C_RESET='\033[0m'
log()  { echo -e "${C_GREEN}==>${C_RESET} $*"; }
warn() { echo -e "${C_YELLOW}!! ${C_RESET}$*" >&2; }
err()  { echo -e "${C_RED}!! ${C_RESET}$*" >&2; }
dim()  { echo -e "${C_DIM}$*${C_RESET}"; }

command -v jq >/dev/null 2>&1 || { err "нужен jq"; exit 1; }
[ -f "$WIRE" ] || { err "нет $WIRE — обнови платформу (adc update)"; exit 1; }

# ── Слои: человеческие подписи и файлы ───────────────────────
# Подпись отвечает на два вопроса сразу — «чьё это» и «где это видно», потому
# что именно их человек и задаёт, глядя на незнакомый сервер в списке.
scope_title() {
  case "$1" in
    global)  echo "ГЛОБАЛЬНЫЕ" ;;
    user)    echo "ПОЛЬЗОВАТЕЛЬСКИЕ" ;;
    project) echo "ПРОЕКТНЫЕ" ;;
    local)   echo "ЛОКАЛЬНЫЕ" ;;
  esac
}
scope_note() {
  case "$1" in
    global)  echo "платформа, приезжают с devcontainer'ом · только чтение" ;;
    user)    echo "твои, во всех твоих проектах" ;;
    project) echo "команды, лежат в гите проекта" ;;
    local)   echo "твои, только в этом проекте" ;;
  esac
}
scope_file() {
  case "$1" in
    user)    echo "$USER_STORE/servers.json" ;;
    project) echo "$REPO_ROOT/.agents/mcp.json" ;;
    local)   echo "$REPO_ROOT/.agents/mcp.local.json" ;;
    global)  echo "${AI_DEVCONTAINER_MCP:-$PLATFORM_ROOT/mcp/servers.json}" ;;
  esac
}
secrets_file() {
  case "$1" in
    user) echo "$USER_STORE/secrets.env" ;;
    *)    echo "$REPO_ROOT/.agents/mcp.secrets.env" ;;
  esac
}

# Путь показываем коротким там, где он и так очевиден: строка «в гите проекта»
# рядом с абсолютным /workspaces/... — это шум, про который просили отдельно.
short_path() {
  case "$1" in
    "$REPO_ROOT"/*) echo "${1#"$REPO_ROOT"/}" ;;
    *) echo "$1" ;;
  esac
}

plan() {
  # Движок пишет диагностику в stderr; здесь она человеку не нужна — он просит
  # показать картину, а не чинить раздачу. Ошибку разбора ловим ниже по пустоте.
  REPO_ROOT="$REPO_ROOT" bash "$WIRE" --dump-plan 2>/dev/null || true
}

require_plan() {
  local p; p="$(plan)"
  if [ -z "$p" ] || ! echo "$p" | jq -e . >/dev/null 2>&1; then
    err "движок не отдал план — запусти 'adc mcp sync' и посмотри, на чём он ругается"
    exit 1
  fi
  if echo "$p" | jq -e '.platform_repo == true' >/dev/null 2>&1; then
    dim "это репозиторий платформы — MCP тут не раздаются (см. AGENTS.md)"
    exit 0
  fi
  echo "$p"
}

# ── list ─────────────────────────────────────────────────────
# Плоский текст, без рамок: вывод уходит в чужие пасты, в лог postCreate и в
# issue, где ANSI-рамка превращается в мусор, а колонки — нет.
cmd_list() {
  local p; p="$(require_plan)"
  local width; width="$(tput cols 2>/dev/null || echo 100)"
  [ "$width" -lt 60 ] && width=60

  echo
  echo -e "${C_BOLD}MCP-серверы${C_RESET} · $(basename "$REPO_ROOT")"

  local scope title note file shown any
  for scope in global user project local; do
    title="$(scope_title "$scope")"; note="$(scope_note "$scope")"
    file="$(short_path "$(scope_file "$scope")")"
    echo
    echo -e "${C_BOLD}${title}${C_RESET} ${C_DIM}· ${note}${C_RESET}"
    dim "  $file"

    any=0
    # Сервер показываем в том слое, который им ВЛАДЕЕТ (самый приоритетный из
    # объявивших). Иначе перекрытый playwright висел бы в двух местах разом и
    # вопрос «где мне это править» снова остался бы без ответа.
    while IFS=$'\t' read -r name state detail; do
      [ -n "$name" ] || continue
      any=1
      shown="$(printf '%-22s' "$name")"
      case "$state" in
        active)   echo -e "  ${C_GREEN}●${C_RESET} ${shown} ${C_DIM}${detail}${C_RESET}" ;;
        off)      echo -e "  ${C_DIM}○${C_RESET} ${shown} ${C_YELLOW}${detail}${C_RESET}" ;;
      esac
    done < <(echo "$p" | jq -r --arg scope "$scope" --argjson w "$((width - 28))" '
      def summary($s):
        (if $s.url then ($s.url | sub("(?<p>[?&](access_token|token|api_key)=)[^&]+"; "\(.p)***"))
         else (([$s.command] + ($s.args // [])) | join(" ")) end)
        | if (. | length) > $w then (.[0:$w-1] + "…") else . end;

      . as $plan
      # Список имён берём у провенанса, а не у самого слоя: сервер, выключенный
      # через null, в servers слоя отсутствует — и, если спрашивать слой,
      # исчезал бы с экрана целиком вместо честного «○ выключен».
      | ($plan.origins | to_entries | map(select(.value.scope == $scope) | .key) | sort) as $names
      | $names[]
      | . as $n
      | if ($plan.active | index($n)) then
          [$n, "active", summary($plan.defs[$n])]
        elif ($plan.disabled[$n] // null) != null then
          [$n, "off", "выключен в слое «\($plan.disabled[$n])»"]
        else
          [$n, "off", ($plan.reasons[$n].reason // "не раздан")]
        end
      | @tsv')

    # Пустой слой — не молчание, а приглашение: человек в этот момент как раз и
    # ищет, куда класть своё.
    if [ "$any" = 0 ]; then
      case "$scope" in
        global) dim "  (пусто — платформа не раздаёт ни одного сервера)" ;;
        *)      dim "  (пусто)   adc mcp add <имя> --$scope" ;;
      esac
    fi
  done

  # Сервер, перекрытый сверху, виден в своём слое-владельце — но факт
  # перекрытия надо назвать, иначе «почему у меня playwright не такой, как у
  # коллеги» превращается в получасовой квест.
  local shadowed
  shadowed="$(echo "$p" | jq -r '
    .origins | to_entries[] | select(.value.shadows | length > 0)
    | "  \(.key): слой «\(.value.scope)» перекрывает \(.value.shadows | join(", "))"')"
  if [ -n "$shadowed" ]; then
    echo
    echo -e "${C_BOLD}ПЕРЕКРЫТИЯ${C_RESET}"
    echo "$shadowed"
  fi

  echo
  echo -e "${C_GREEN}●${C_RESET} активен  ${C_DIM}○${C_RESET} выключен"
  dim "adc mcp add <имя> — добавить · adc mcp enable <имя> — включить · adc mcp sync — применить"
  echo
}

# ── Куда писать: разбор --user/--project/--local ─────────────
# Слой не угадываем молча. У TTY спрашиваем, без TTY берём самый безобидный:
# локальный не уезжает ни в гит команды, ни в остальные проекты. Тихо положить
# личный сервер в общий гит — ровно та ошибка, которую потом никто не заметит.
pick_scope() {
  local given="$1"
  if [ -n "$given" ]; then echo "$given"; return 0; fi
  if [ ! -t 0 ]; then
    warn "слой не указан — беру локальный (только этот проект). Явно: --user | --project | --local" >&2
    echo local; return 0
  fi
  {
    echo "Куда положить сервер?"
    echo "  1) локальный  — только я, только этот проект      (.agents/mcp.local.json)"
    echo "  2) пользовательский — только я, во всех проектах  ($USER_STORE/servers.json)"
    echo "  3) проектный  — вся команда, уедет в гит          (.agents/mcp.json)"
  } >&2
  local ans
  read -r -p "[1] > " ans >&2 || ans=""
  case "${ans:-1}" in
    1|"") echo local ;;
    2)    echo user ;;
    3)    echo project ;;
    *)    err "не понял «$ans»"; exit 1 ;;
  esac
}

# Запись в слой. Пользовательский стор может быть недоступен (команду позвали
# на хосте, где /opt/ai-tools не смонтирован) — говорим об этом прямо, а не
# падаем с EACCES из недр jq.
ensure_layer_writable() {
  local scope="$1" file; file="$(scope_file "$scope")"
  [ "$scope" = global ] && { err "глобальный слой — платформенный, руками его не правят (см. AGENTS.md)"; exit 1; }
  local dir; dir="$(dirname "$file")"
  if [ ! -d "$dir" ] && ! mkdir -p "$dir" 2>/dev/null; then
    err "нет доступа к $dir"
    [ "$scope" = user ] && dim "  пользовательский слой живёт в /opt/ai-tools — он есть только внутри контейнера"
    exit 1
  fi
  [ -f "$file" ] || echo '{"mcpServers":{}}' > "$file"
  [ -w "$file" ] || { err "файл только для чтения: $file"; exit 1; }
}

write_server() {
  local scope="$1" name="$2" body="$3" file tmp
  file="$(scope_file "$scope")"
  tmp="$file.tmp.$$"
  jq --arg n "$name" --argjson v "$body" '.mcpServers = ((.mcpServers // {}) + {($n): $v})' \
     "$file" > "$tmp" && mv "$tmp" "$file"
}

# ── Секреты, вписанные литералом ─────────────────────────────
# Проектный слой — единственный из четырёх, который лежит В ГИТЕ: в этом его
# смысл. Значит вписанный туда токен уезжает всей команде и в историю, откуда
# его уже не вынуть — только отзывать. Поэтому литералы там ловим и выносим.
#
# Эвристика по имени поля плюс форма значения (Bearer/Basic + длинный хвост).
# Значение с ${} не трогаем: это и есть правильный способ.
SECRET_KEY_RE='authorization|token|api[_-]?key|apikey|secret|password|passwd'

# Печатает TSV: путь-через-точку \t значение — для каждого литерального секрета.
find_literal_secrets() {
  jq -r --arg re "$SECRET_KEY_RE" '
    def looks_secret($k; $v):
      ($k | ascii_downcase | test($re))
      or ($v | test("^(Bearer|Basic|Token)[[:space:]]+[^[:space:]]{12,}$"));
    paths(type == "string") as $p
    | { k: ($p[-1] | tostring), v: getpath($p), path: ($p | map(tostring) | join(".")) }
    | select(.v | test("\\$\\{") | not)
    | select(.v | length > 0)
    | select(looks_secret(.k; .v))
    | [.path, .v] | @tsv' "$1" 2>/dev/null || true
}

# Имя переменной из имени сервера и поля: DIRECTUS_AUTHORIZATION и т.п.
# Читаемое имя важнее короткого — оно потом стоит в .agents/mcp.secrets.env,
# и человек должен понимать, к чему эта строка, без сверки с конфигом.
secret_var_name() {
  local server="$1" field="$2" name
  name="$(printf '%s_%s' "$server" "$field" | tr 'a-z-' 'A-Z_' | tr -cd 'A-Z0-9_')"
  printf '%s' "$name"
}

# Массив строк в JSON. Через `jq --args` не выходит: jq продолжает разбирать
# опции после фильтра и спотыкается о первый же аргумент, начинающийся с «-».
json_array() {
  [ "$#" -eq 0 ] && { echo '[]'; return 0; }
  printf '%s\n' "$@" | jq -R . | jq -sc .
}

valid_name() {
  # Общий знаменатель четырёх агентов: у DSH имя сервера обязано быть
  # [A-Za-z0-9_-]{1,32}, иначе он молча пропускает сервер целиком.
  case "$1" in
    *[!A-Za-z0-9_-]*|"") return 1 ;;
  esac
  [ "${#1}" -le 32 ]
}

# ── add ──────────────────────────────────────────────────────
cmd_add() {
  local name="" scope="" url="" json="" cmd_args=() headers=()
  while [ $# -gt 0 ]; do
    case "$1" in
      --user|--project|--local) scope="${1#--}"; shift ;;
      --global) err "в глобальный слой пишет только платформа"; exit 1 ;;
      --url)    url="${2:-}"; shift 2 ;;
      --header) headers+=("${2:-}"); shift 2 ;;
      --json)   json="${2:-}"; shift 2 ;;
      --)       shift; cmd_args=("$@"); break ;;
      -*)       err "неизвестный флаг: $1"; exit 1 ;;
      *)        [ -z "$name" ] && name="$1" || { err "лишний аргумент: $1"; exit 1; }; shift ;;
    esac
  done

  # Вставка сниппета из README сервера — главный сценарий: у всех MCP-серверов
  # в документации лежит готовый {"mcpServers": {...}}, и переписывать его
  # руками в наш формат — ровно та работа, которой тут быть не должно.
  local body=""
  if [ -n "$json" ]; then
    echo "$json" | jq -e . >/dev/null 2>&1 || { err "--json: это не JSON"; exit 1; }
    if echo "$json" | jq -e '.mcpServers' >/dev/null 2>&1; then
      local count; count="$(echo "$json" | jq -r '.mcpServers | length')"
      if [ -z "$name" ]; then
        [ "$count" = 1 ] || { err "в сниппете $count серверов — назови нужный: adc mcp add <имя> --json ..."; exit 1; }
        name="$(echo "$json" | jq -r '.mcpServers | keys[0]')"
      fi
      body="$(echo "$json" | jq -c --arg n "$name" '.mcpServers[$n] // empty')"
      [ -n "$body" ] || { err "в сниппете нет сервера «$name»"; exit 1; }
    else
      body="$(echo "$json" | jq -c .)"
    fi
  elif [ ${#cmd_args[@]} -gt 0 ]; then
    # Через --args не собрать: jq продолжает разбирать опции и на первом же
    # `-y` (а он есть в каждой второй команде npx) падает с «Unknown option».
    body="$(jq -nc --arg c "${cmd_args[0]}" --argjson a "$(json_array "${cmd_args[@]:1}")" \
              '{command: $c, args: $a}')"
  elif [ -n "$url" ]; then
    body="$(jq -nc --arg u "$url" '{type: "http", url: $u}')"
    local h k v
    for h in ${headers+"${headers[@]}"}; do
      k="${h%%:*}"; v="${h#*:}"; v="${v# }"
      [ "$k" = "$h" ] && { err "--header ждёт «Имя: значение», получил «$h»"; exit 1; }
      body="$(echo "$body" | jq -c --arg k "$k" --arg v "$v" '.headers = ((.headers // {}) + {($k): $v})')"
    done
  else
    err "нечего добавлять. Как это делается:"
    {
      echo "  adc mcp add --json '<сниппет из README сервера>'"
      echo "  adc mcp add linear -- npx -y linear-mcp-server"
      echo "  adc mcp add linear --url https://mcp.example.com/mcp --header 'Authorization: Bearer \${TOKEN}'"
    } >&2
    exit 1
  fi

  [ -n "$name" ] || { err "не указано имя сервера"; exit 1; }
  valid_name "$name" || { err "имя «$name»: допустимы A-Za-z0-9_- до 32 символов (ограничение DSH)"; exit 1; }

  scope="$(pick_scope "$scope")"
  ensure_layer_writable "$scope"

  # Литеральный секрет в проектный слой не пускаем: этот файл лежит в гите, и
  # вписанный токен уедет всей команде и в историю. Не отказываем — молча
  # делаем правильно: значение в секреты, в слой ${ИМЯ}. Отказ тут был бы хуже:
  # человек скопировал рабочий сниппет и не обязан знать про наши слои.
  if [ "$scope" = project ]; then
    local sfile spath svalue sfield svar sscheme ssecret
    sfile="$(secrets_file "$scope")"
    while IFS=$'\t' read -r spath svalue; do
      [ -n "$spath" ] || continue
      sfield="$(printf '%s' "$spath" | awk -F. '{print $NF}')"
      svar="$(secret_var_name "$name" "$sfield")"
      sscheme=""; ssecret="$svalue"
      if [[ "$svalue" =~ ^(Bearer|Basic|Token)[[:space:]]+(.+)$ ]]; then
        sscheme="${BASH_REMATCH[1]} "; ssecret="${BASH_REMATCH[2]}"
      fi
      mkdir -p "$(dirname "$sfile")"
      touch "$sfile" && chmod 600 "$sfile" 2>/dev/null || true
      grep -qE "^[[:space:]]*(export[[:space:]]+)?${svar}=" "$sfile" 2>/dev/null \
        || printf '%s=%s\n' "$svar" "$ssecret" >> "$sfile"
      body="$(echo "$body" | jq -c --arg p "$spath" --arg repl "${sscheme}\${${svar}}" \
              'setpath($p | split(".") | map(if test("^[0-9]+$") then tonumber else . end); $repl)')"
      warn "секрет в $sfield вынесен в \${$svar} — слой команды лежит в гите"
    done < <(printf '%s' "$body" | find_literal_secrets /dev/stdin)
  fi

  local file; file="$(scope_file "$scope")"
  if jq -e --arg n "$name" '.mcpServers[$n]' "$file" >/dev/null 2>&1; then
    warn "«$name» в слое «$scope» уже есть — перезаписываю"
  fi
  write_server "$scope" "$name" "$body"
  log "«$name» → слой «$scope» ($(short_path "$file"))"

  # Подстановки, которым неоткуда взяться, — самая частая причина «добавил, а
  # оно не работает». Спрашиваем сразу, пока человек в контексте.
  local missing
  missing="$(echo "$body" | jq -r '[paths(type=="string") as $p | getpath($p)
              | scan("\\$\\{([A-Za-z_][A-Za-z0-9_]*)\\}")] | flatten | unique | .[]')"
  local var
  for var in $missing; do
    [ -n "${!var:-}" ] && continue
    prompt_secret "$scope" "$var"
  done

  run_sync
}

# ── rm ───────────────────────────────────────────────────────
cmd_rm() {
  local name="" scope=""
  while [ $# -gt 0 ]; do
    case "$1" in
      --user|--project|--local) scope="${1#--}"; shift ;;
      --global) err "глобальный слой правит только платформа"; exit 1 ;;
      -*) err "неизвестный флаг: $1"; exit 1 ;;
      *)  name="$1"; shift ;;
    esac
  done
  [ -n "$name" ] || { err "использование: adc mcp rm <имя> [--user|--project|--local]"; exit 1; }

  # Слой не спрашиваем, а находим: сервер лежит там, где лежит, и заставлять
  # человека помнить это — та же болезнь, что мы лечим.
  if [ -z "$scope" ]; then
    local s found=""
    for s in local project user; do
      local f; f="$(scope_file "$s")"
      [ -f "$f" ] || continue
      jq -e --arg n "$name" '.mcpServers | has($n)' "$f" >/dev/null 2>&1 && { found="$s"; break; }
    done
    [ -n "$found" ] || { err "«$name» не найден ни в одном правимом слое (adc mcp list)"; exit 1; }
    scope="$found"
  fi

  local file tmp; file="$(scope_file "$scope")"
  jq -e --arg n "$name" '.mcpServers | has($n)' "$file" >/dev/null 2>&1 \
    || { err "«$name» нет в слое «$scope»"; exit 1; }
  tmp="$file.tmp.$$"
  jq --arg n "$name" 'del(.mcpServers[$n])' "$file" > "$tmp" && mv "$tmp" "$file"
  log "«$name» убран из слоя «$scope»"
  run_sync
}

# ── enable / disable ─────────────────────────────────────────
# Спросить значение и положить его в нужный файл секретов. Ввод не эхоим: это
# токен, а история терминала и скроллбек живут дольше, чем кажется.
prompt_secret() {
  local scope="$1" var="$2" sfile val
  sfile="$(secrets_file "$scope")"
  if [ ! -t 0 ]; then
    warn "нужна переменная $var — впиши её в $(short_path "$sfile") и запусти adc mcp sync"
    return 0
  fi
  echo -e "${C_DIM}нужна переменная ${var} → $(short_path "$sfile")${C_RESET}" >&2
  read -r -s -p "$var = " val >&2 || val=""
  echo >&2
  [ -n "$val" ] || { warn "пусто — пропускаю $var"; return 0; }

  mkdir -p "$(dirname "$sfile")" 2>/dev/null || { err "нет доступа к $(dirname "$sfile")"; return 1; }
  touch "$sfile" && chmod 600 "$sfile" 2>/dev/null || true
  # Существующую строку заменяем, а не дописываем вторую: дубль ключа движок
  # разрешает в пользу ПОСЛЕДНЕЙ строки, и человек правил бы верхнюю, глядя,
  # как ничего не меняется.
  if grep -qE "^[[:space:]]*(export[[:space:]]+)?${var}=" "$sfile" 2>/dev/null; then
    local tmp="$sfile.tmp.$$"
    grep -vE "^[[:space:]]*(export[[:space:]]+)?${var}=" "$sfile" > "$tmp" || true
    printf '%s=%s\n' "$var" "$val" >> "$tmp"
    mv "$tmp" "$sfile" && chmod 600 "$sfile" 2>/dev/null || true
  else
    printf '%s=%s\n' "$var" "$val" >> "$sfile"
  fi
  log "$var записан в $(short_path "$sfile")"
}

cmd_enable() {
  local name="${1:-}" scope="${2:-}"
  [ -n "$name" ] || { err "использование: adc mcp enable <имя> [--user|--project]"; exit 1; }
  case "$scope" in --user) scope=user ;; --project) scope=project ;; "") scope=user ;;
                   *) err "неизвестный флаг: $scope"; exit 1 ;; esac

  local p; p="$(require_plan)"
  if echo "$p" | jq -e --arg n "$name" '.active | index($n)' >/dev/null 2>&1; then
    log "«$name» и так активен"; return 0
  fi

  # Выключен через null в каком-то слое — снимаем выключатель, а не гадаем.
  local off; off="$(echo "$p" | jq -r --arg n "$name" '.disabled[$n] // ""')"
  if [ -n "$off" ]; then
    local f tmp; f="$(scope_file "$off")"; tmp="$f.tmp.$$"
    jq --arg n "$name" 'del(.mcpServers[$n])' "$f" > "$tmp" && mv "$tmp" "$f"
    log "«$name» больше не выключен в слое «$off»"
    # Пересчитываем: снятый выключатель мог оказаться не единственной причиной,
    # и «включил, а его нет» — худший из возможных ответов на enable.
    p="$(require_plan)"
    if echo "$p" | jq -e --arg n "$name" '.active | index($n)' >/dev/null 2>&1; then
      run_sync; return 0
    fi
  fi

  local need_env need_path
  need_env="$(echo "$p" | jq -r --arg n "$name" '.reasons[$n].need_env // ""')"
  need_path="$(echo "$p" | jq -r --arg n "$name" '.reasons[$n].need_path // ""')"

  if [ -n "$need_path" ]; then
    err "«$name» требует путь, которого нет: $need_path"
    dim "  это не настройка, а отсутствующий в окружении файл — правится не здесь"
    exit 1
  fi
  if [ -z "$need_env" ]; then
    err "«$name» нет ни в одном слое (adc mcp list покажет, что есть)"
    exit 1
  fi
  prompt_secret "$scope" "$need_env"
  run_sync
}

cmd_disable() {
  local name="${1:-}" scope="${2:-local}"
  [ -n "$name" ] || { err "использование: adc mcp disable <имя> [--local|--project|--user]"; exit 1; }
  case "$scope" in --user) scope=user ;; --project) scope=project ;; --local|local) scope=local ;;
                   *) err "неизвестный флаг: $scope"; exit 1 ;; esac
  ensure_layer_writable "$scope"
  # Выключение — это `null` поверх нижнего слоя, а не удаление: платформенный
  # сервер удалить всё равно нельзя, а перекрыть — можно.
  local file tmp; file="$(scope_file "$scope")"; tmp="$file.tmp.$$"
  jq --arg n "$name" '.mcpServers = ((.mcpServers // {}) + {($n): null})' "$file" > "$tmp" && mv "$tmp" "$file"
  log "«$name» выключен в слое «$scope»"
  run_sync
}

# Раздача после правки слоя. Хронику глушим: человек только что попросил
# «добавь сервер», и двадцать строк про остальные серверы — это тот самый шум,
# из-за которого вывод перестают читать. Итог и предупреждения остаются.
# ── fix-secrets ──────────────────────────────────────────────
# Вынести литеральные секреты из слоя в файл секретов, оставив ${ИМЯ}.
# Значение сохраняем целиком, кроме схемы: «Bearer xxx» → «Bearer ${VAR}», а не
# «${VAR}» с Bearer'ом внутри — иначе переменная перестаёт быть просто токеном
# и её нельзя переиспользовать в другом сервере.
cmd_fix_secrets() {
  local scope="${1:---project}"
  case "$scope" in --user) scope=user ;; --project|"") scope=project ;; --local) scope=local ;;
                   *) err "неизвестный флаг: $scope"; exit 1 ;; esac

  local file sfile; file="$(scope_file "$scope")"; sfile="$(secrets_file "$scope")"
  [ -f "$file" ] || { log "слой «$scope» пуст — выносить нечего"; return 0; }

  local found=0 path value server field var scheme secret tmp
  while IFS=$'\t' read -r path value; do
    [ -n "$path" ] || continue
    found=1
    # path вида mcpServers.<сервер>.headers.Authorization
    server="$(printf '%s' "$path" | cut -d. -f2)"
    field="$(printf '%s' "$path" | awk -F. '{print $NF}')"
    var="$(secret_var_name "$server" "$field")"

    scheme=""; secret="$value"
    if [[ "$value" =~ ^(Bearer|Basic|Token)[[:space:]]+(.+)$ ]]; then
      scheme="${BASH_REMATCH[1]} "; secret="${BASH_REMATCH[2]}"
    fi

    mkdir -p "$(dirname "$sfile")"
    touch "$sfile" && chmod 600 "$sfile" 2>/dev/null || true
    if grep -qE "^[[:space:]]*(export[[:space:]]+)?${var}=" "$sfile" 2>/dev/null; then
      warn "$var уже есть в $(short_path "$sfile") — оставляю прежнее значение"
    else
      printf '%s=%s\n' "$var" "$secret" >> "$sfile"
    fi

    tmp="$file.tmp.$$"
    jq --arg p "$path" --arg repl "${scheme}\${${var}}" \
       'setpath($p | split(".") | map(if test("^[0-9]+$") then tonumber else . end); $repl)' \
       "$file" > "$tmp" && mv "$tmp" "$file"
    log "$path → \${$var} (значение в $(short_path "$sfile"))"
  done < <(find_literal_secrets "$file")

  if [ "$found" = 0 ]; then
    log "литеральных секретов в слое «$scope» нет"
    return 0
  fi
  warn "если слой уже закоммичен — токен надо ОТОЗВАТЬ: он в истории гита"
  run_sync
}

run_sync() {
  MCP_QUIET=1 REPO_ROOT="$REPO_ROOT" bash "$WIRE"
  dim "перезапусти агента (claude / codex / hermes / dsh) — серверы читаются на старте"
}

usage() {
  cat >&2 <<'U'
adc mcp — MCP-серверы проекта

  list                        показать все слои и что в них активно
  add <имя> [слой] ...        добавить сервер
  rm <имя> [слой]             убрать сервер
  enable <имя> [слой]         включить (спросит недостающий токен)
  disable <имя> [слой]        выключить, перекрыв нижний слой
  sync                        пересобрать конфиги агентов
  fix-secrets [слой]          вынести вписанные литералом токены в секреты

Слои (снизу вверх; проектное бьёт глобальное, моё бьёт общее):
  --global    платформа, приезжает с devcontainer'ом      только чтение
  --user      мои серверы во всех моих проектах
  --project   серверы команды, уедут в гит
  --local     мои серверы только в этом проекте           по умолчанию

Добавить сервер тремя способами:
  adc mcp add --json '<сниппет из README сервера>'
  adc mcp add linear -- npx -y linear-mcp-server
  adc mcp add linear --url https://mcp.example.com/mcp --header 'Authorization: Bearer ${TOKEN}'
U
}

case "${1:-list}" in
  list)    shift; cmd_list "$@" ;;
  add)     shift; cmd_add "$@" ;;
  rm|remove) shift; cmd_rm "$@" ;;
  enable)  shift; cmd_enable "$@" ;;
  disable) shift; cmd_disable "$@" ;;
  sync)    shift; run_sync ;;
  fix-secrets) shift; cmd_fix_secrets "$@" ;;
  help|-h|--help) usage ;;
  *) err "неизвестная команда: $1"; usage; exit 1 ;;
esac
