#!/usr/bin/env bash
# tooling/wire-mcp.sh — раздаёт MCP-серверы на всех агентов проекта.
#
# Та же болезнь, что была со скиллами: MCP настраивается У АГЕНТА, а не в
# проекте, причём у каждого агента по-своему и в РАЗНОМ месте. Руками это
# означает три конфига на проект и полный разъезд между проектами.
#
# ДВА СЛОЯ (overlay), приоритет — проектный, как у скиллов:
#   1. платформенный  $PLATFORM_ROOT/mcp/servers.json   (общий для всех проектов)
#   2. проектный      <repo>/.agents/mcp.json           (только отличия)
#
# Перекрытие ПОСЕРВЕРНОЕ и рекурсивное: проект может переопределить у
# платформенного сервера только `env`, оставив `command`/`args` платформенными.
# Массивы заменяются целиком (склеивать args бессмысленно). Значение `null`
# вместо объекта = выключить платформенный сервер в этом проекте.
#
# Формат обоих слоёв — `{"mcpServers": {"<имя>": {...}}}`, тот же, что у
# Claude Code. Ключи, начинающиеся с `//`, — комментарии, вырезаются.
#
# ПОДСТАНОВКИ. В любом строковом значении раскрывается `${ИМЯ}`:
#   • REPO_ROOT             — абсолютный корень проекта (cwd у трёх агентов
#                             разный, серверу нужен абсолютный путь);
#   • всё из <repo>/.agents/mcp.secrets.env (KEY=VALUE, файл в .gitignore);
#   • всё из окружения процесса.
# Приоритет: secrets-файл > окружение. Нераскрытое `${ИМЯ}` остаётся текстом —
# и почти всегда означает, что сервер надо было отсечь через x-requires.
#
# СЕКРЕТЫ РАСКРЫВАЮТСЯ ЗДЕСЬ, а не оставляются агенту. Причина практическая:
# `${VAR}` умеют раскрывать Claude и Hermes, а на Codex это не проверено, и
# половинчатая схема (у двоих работает, у третьего молча пусто) хуже честной.
# Цена: токен лежит плейнтекстом в .mcp.json и ~/.codex/config.toml. Это не
# новый класс риска — в тех же каталогах уже лежат ЛОГИНЫ самих агентов, и
# ничего из этого не в гите (.mcp.json в .gitignore, home пер-проектный).
# Файлы с секретами пишем с правами 600.
#
# x-requires — условия раздачи сервера, список строк:
#   • "env:ИМЯ"    — переменная должна быть определена и непуста;
#   • "path:/путь" — файл/каталог должен существовать (${} в пути раскрывается).
# Не выполнено ХОТЯ БЫ ОДНО — сервер не раздаём. Это нужно, потому что слой
# платформы общий: без гейта репозиторий получал бы сервер,
# падающий у агента на первом же вызове. Пропуск логируется, не молчит.
#
# КУДА РАЗДАЁМ (у первых трёх формат записи совпал — command/args/env либо url;
# у DSH свой, разбор ниже):
#   - Claude Code — <repo>/.mcp.json, единственный из четырёх с проектным скоупом
#   - Codex       — ~/.codex/config.toml через `codex mcp add` (home-скоуп)
#   - Hermes      — ~/.hermes/config.yaml, ключ mcp_servers (home-скоуп)
#   - DSH         — ~/.dsh/cordis.patch.yml, patch-op `insert` (home-скоуп)
#
# Home-скоуп у трёх из четырёх не мешает: в devcontainer'е ~/.codex, ~/.hermes
# и ~/.dsh — ПЕР-ПРОЕКТНЫЕ bind-маунты, так что проекты за них не дерутся. На
# голом хосте два проекта перетрут друг друга — цена осознанная: своего
# проектного скоупа у этих агентов просто нет.
#
# У DSH формат СВОЙ и не похож на остальные три: не словарь серверов, а список
# патчей композиции Cordis, где каждый сервер — отдельный инстанс плагина
# `@deepseek-ai/dsh-mcp-client`. Наш `{command,args,env}` ложится на его
# `transport: stdio`, а `{url,headers}` — на `transport: streamable-http`;
# заголовки он, в отличие от Codex, умеет штатно. `!!js process.env.X` в
# значениях не используем: подстановки мы раскрываем сами, и литерал честнее.
# Свой блок держим в маркерах — файл общий, человек может вписать туда своё.
#
# Hermes пишем НАПРЯМУЮ в config.yaml, а не через `hermes mcp add`: последний
# интерактивен (спрашивает y/N, если сервер не поднялся) и коннектится к
# серверу на этапе добавления — в postCreate без TTY это висяк.
# OpenCode не раздаём: у него свой формат (`opencode.json`, ключ `mcp`).
#
# x-oauth: true — сервер авторизуется по OAuth. Метка нужна ровно для Codex:
# такой сервер туда НЕ добавляется автоматически, мы только печатаем команду.
# Причин две: логин там всё равно проходит человек, и исторически
# `codex mcp add --url` коннектился к серверу прямо при добавлении, поднимая
# интерактивный запрос авторизации — в postCreate это висяк. На codex-cli
# 0.142.4 add уже не коннектится, но политика осталась: цена ошибки — зависший
# postCreate у всех, кто заведёт OAuth-сервер, а выгода — одна ручная команда.
# ВАЖНО: метка не про url вообще — streamable-HTTP сервер без OAuth
# добавляется в Codex как все.
#
# Отдельно от OAuth в Codex не едут серверы с `headers`: заголовки он умеет
# только ключом http_headers в config.toml, а `codex mcp add` его не выставляет.
# Приписать блок к config.toml самим — значит на каждом sync ловить дубль
# таблицы (add переписывает [mcp_servers.X] целиком, снося вложенные) и чинить
# TOML руками. Дешевле напечатать готовый блок и дать вписать его человеку.
#
# OAUTH-СЕРВЕРЫ, кроме того, не стоит раздавать без явного согласия: попав в
# конфиг, такой сервер просит авторизацию при КАЖДОМ старте агента во всех
# проектах. Поэтому figma в платформенном слое сидит ещё и за
# x-requires: env:FIGMA_MCP_ENABLED и по умолчанию не materialize'ится.
#
# Идемпотентно. Список того, что разложили мы, лежит в
# <repo>/.claude/.ai-devcontainer-mcp — по нему чистятся серверы, уехавшие из
# конфигов платформы и проекта. Чужие MCP-серверы не трогаем никогда.

set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$PWD}"
TOOLING_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PLATFORM_ROOT="$(dirname "$TOOLING_DIR")"
cd "$REPO_ROOT"

# ЧЕТЫРЕ СЛОЯ, снизу вверх по приоритету. Порядок не произволен: проектное
# бьёт глобальное, моё бьёт общее — обе оси упорядочены одинаково, поэтому
# правило запоминается одной фразой и не требует сверки с таблицей.
#
#   глобальный      платформа, раздаётся devcontainer'ом  (общий,   везде)
#   пользовательский мои серверы во всех моих проектах     (личный,  везде)
#   проектный        серверы команды, лежат в гите         (общий,   здесь)
#   локальный        мои серверы только в этом проекте     (личный,  здесь)
#
# Имена слоёв — те же, что у `claude mcp add --scope`: человек, знающий любой
# из четырёх агентов, читает вывод без перевода.
PLATFORM_MCP="${AI_DEVCONTAINER_MCP:-$PLATFORM_ROOT/mcp/servers.json}"
# Пользовательский слой живёт в /opt/ai-tools — том platform-ai-tools общий на
# все проекты и уже примонтирован даже в заведённые до этой функции, так что
# devcontainer.json править не пришлось. Тот же трюк, что у hermes-auth.sh.
USER_STORE="${AI_DEVCONTAINER_MCP_STORE:-/opt/ai-tools/share/mcp}"
USER_MCP="$USER_STORE/servers.json"
USER_SECRETS="$USER_STORE/secrets.env"
PROJECT_MCP="$REPO_ROOT/.agents/mcp.json"
LOCAL_MCP="$REPO_ROOT/.agents/mcp.local.json"
SECRETS_FILE="$REPO_ROOT/.agents/mcp.secrets.env"
CLAUDE_MCP="$REPO_ROOT/.mcp.json"
HERMES_CONFIG="${HERMES_HOME:-$HOME/.hermes}/config.yaml"
DSH_HOME_DIR="${DSH_HOME:-$HOME/.dsh}"
DSH_PATCH="$DSH_HOME_DIR/cordis.patch.yml"
STATE_FILE="$REPO_ROOT/.claude/.ai-devcontainer-mcp"

C_GREEN='\033[0;32m'; C_YELLOW='\033[0;33m'; C_DIM='\033[2m'; C_RESET='\033[0m'
log()  { echo -e "${C_GREEN}==>${C_RESET} $*"; }
warn() { echo -e "${C_YELLOW}!! ${C_RESET}$*" >&2; }
dim()  { echo -e "${C_DIM}$*${C_RESET}"; }

# Два режима. Без аргументов — раздача (всё, что было). `--dump-plan` считает
# ровно то же самое и печатает результат JSON'ом, НИЧЕГО не записывая: на нём
# живёт `adc mcp list`. Отдельного «своего» мерджа у list'а нет намеренно —
# картинка, расходящаяся с реальностью, хуже отсутствия картинки.
MODE="sync"
case "${1:-}" in
  ""|sync)     MODE="sync" ;;
  --dump-plan) MODE="plan" ;;
  *) echo "wire-mcp.sh: неизвестный аргумент «$1» (ожидаю: sync | --dump-plan)" >&2; exit 2 ;;
esac

command -v jq >/dev/null 2>&1 || { warn "нет jq — MCP не раздаю"; [ "$MODE" = plan ] && echo '{"error":"no-jq"}'; exit 0; }

# В режиме плана stdout занят JSON — человеческий вывод там неуместен целиком
# (warn и так уходит в stderr, а log/dim писали в stdout и порвали бы разбор).
if [ "$MODE" = plan ]; then
  log() { :; }
  dim() { :; }
fi

# MCP_QUIET=1 — гасим построчную хронику раздачи, оставляя итог и предупреждения.
# Нужен, когда раздачу дёргает не человек, а соседняя команда (`adc mcp add`):
# там на экране важно «сервер добавлен», а не двадцать строк про остальные
# серверы, к которым человек сейчас отношения не имеет.
if [ "${MCP_QUIET:-}" = 1 ]; then
  dim() { :; }
fi

# Репозиторий платформы, открытый сам в себе — не раздаём, и это не лень.
# У платформы `.agents` — симлинк в `skeleton/.agents`, то есть «проектным
# слоем» тут оказался бы шаблон новых проектов: любое локальное перекрытие
# уехало бы во ВСЕ проекты, созданные дальше. Разводить эти два смысла дороже,
# чем обойтись без MCP при разработке самой платформы.
#
# Сравниваем через cd+pwd, а НЕ через `readlink -f`: этот скрипт зовётся и с
# хоста (`adc mcp list`), а на macOS у readlink нет -f — обе подстановки дают
# пустую строку, сравнение "" = "" совпадает, и платформой объявляется любой
# проект. Тот же приём, что abs_dir() в bin/adc.
abs_dir() { (cd "$1" 2>/dev/null && pwd) || printf '%s' "$1"; }
if [ "$(abs_dir "$REPO_ROOT")" = "$(abs_dir "$PLATFORM_ROOT")" ]; then
  dim "  это репозиторий платформы — MCP не раздаю"
  [ "$MODE" = plan ] && echo '{"platform_repo":true}'
  exit 0
fi

# ── 1. Валидация слоёв ───────────────────────────────────────
# Именно здесь, в основном шелле: ниже слои читаются в подстановке команд, а
# `exit` внутри неё убивает только подоболочку — скрипт поехал бы дальше с
# пустым вводом и неразборчивым «invalid JSON text passed to --argjson».
for layer in "$PLATFORM_MCP" "$USER_MCP" "$PROJECT_MCP" "$LOCAL_MCP"; do
  [ -f "$layer" ] || continue
  jq -e . "$layer" >/dev/null 2>&1 && continue
  warn "невалидный JSON: $layer — MCP не раздаю, пока не починишь"
  exit 1
done

EMPTY='{"mcpServers":{}}'
read_layer() { [ -f "$1" ] && cat "$1" || echo "$EMPTY"; }

# ── 1a. Файл секретов проекта — настоящий, не симлинк ────────
# Рядом лежит ОБРАЗЕЦ, и он симлинк в read-only слой платформы (так список
# переменных не отстаёт от набора стоковых серверов). Копия с него плоским `cp`
# даёт нормальный файл, а вот любая копия, СОХРАНЯЮЩАЯ симлинки — `cp -a`,
# `cp -P`, копипаст в проводнике VS Code — даёт ссылку в /opt/ai-devcontainer.
# Выглядит она как обычный файл ровно до момента сохранения, а потом редактор
# отвечает «EROFS: read-only file system», и понять, при чём тут секреты,
# невозможно. Поэтому файл заводим сами и симлинк на этом месте чиним.
SECRETS_EXAMPLE="$REPO_ROOT/.agents/mcp.secrets.env.example"
# В режиме плана не создаём ничего: `adc mcp list` — это «посмотреть», и
# заводить от него файлы в репозитории человек не просил.
if [ "$MODE" = plan ]; then
  :
elif [ -L "$SECRETS_FILE" ]; then
  warn "$SECRETS_FILE — симлинк (почти наверняка на образец в read-only слое платформы)"
  warn "  так его не отредактировать; заменяю настоящим файлом, содержимое сохраняю"
  tmp="$SECRETS_FILE.real.$$"
  cp -L "$SECRETS_FILE" "$tmp" 2>/dev/null || : > "$tmp"   # битая ссылка → пустой файл
  rm -f "$SECRETS_FILE" && mv "$tmp" "$SECRETS_FILE" && chmod 600 "$SECRETS_FILE"
elif [ ! -e "$SECRETS_FILE" ] && [ -e "$SECRETS_EXAMPLE" ]; then
  # -L обязателен: образец сам симлинк, без него мы бы создали вторую ссылку
  # ровно в ту же read-only копилку и воспроизвели проблему своими руками.
  mkdir -p "$(dirname "$SECRETS_FILE")"
  if cp -L "$SECRETS_EXAMPLE" "$SECRETS_FILE" 2>/dev/null; then
    chmod 600 "$SECRETS_FILE"
    log "завёл .agents/mcp.secrets.env из образца — все строки закомментированы, впиши свои"
  fi
fi

# ── 2. Переменные для подстановки ────────────────────────────
# Секреты — построчный разбор на bash: значения бывают с пробелами, знаками
# `=` и кавычками, наивный split по ним разъезжается, но передача через
# jq --arg безопасна для любого значения. Окружение процесса в JSON руками
# не сериализуем — берём напрямую встроенным $ENV в jq (jq 1.6+, весь
# os.environ процесса без единой строчки парсинга).
parse_secrets_file() {
  local path="$1" json='{}' line key value len first last
  declare -A seen_keys=()
  [ -f "$path" ] || { echo "$json"; return 0; }
  while IFS= read -r line || [ -n "$line" ]; do
    line="${line#"${line%%[![:space:]]*}"}"   # ltrim
    line="${line%"${line##*[![:space:]]}"}"   # rtrim
    [ -z "$line" ] && continue
    [ "${line:0:1}" = "#" ] && continue
    case "$line" in
      export\ *)
        line="${line#export}"
        line="${line#"${line%%[![:space:]]*}"}"
        ;;
    esac
    case "$line" in *=*) ;; *) continue ;; esac
    key="${line%%=*}"                          # только ПЕРВЫЙ `=`
    value="${line#*=}"
    key="${key%"${key##*[![:space:]]}"}"
    value="${value#"${value%%[![:space:]]*}"}"
    [ -z "$key" ] && continue
    case "$key" in [0-9]*) continue ;; esac
    case "$key" in *[!A-Za-z0-9_]*) continue ;; esac
    len=${#value}
    if [ "$len" -ge 2 ]; then
      first="${value:0:1}"; last="${value: -1}"
      if [ "$first" = "$last" ] && { [ "$first" = '"' ] || [ "$first" = "'" ]; }; then
        value="${value:1:len-2}"
      fi
    fi
    # Дубль ключа побеждает молча — последняя строка затирает первую. Ровно
    # так строка, случайно дописанная в конец файла, отменяет значение,
    # которое человек правил сверху и видит своими глазами. Предупреждение —
    # на каждое повторное вхождение (не дедуплицируется при 3+ дублях).
    if [ -n "${seen_keys[$key]+x}" ]; then
      warn "$path: $key задан больше одного раза, в дело идёт последняя строка"
    fi
    seen_keys["$key"]=1
    json="$(jq -n --argjson base "$json" --arg k "$key" --arg v "$value" '$base + {($k): $v}')"
  done < "$path"
  echo "$json"
}

# Два файла секретов, той же цепочкой, что слои серверов: пользовательский
# (один на машину) снизу, проектный сверху — он ближе к задаче, поэтому и
# главнее. Токен, нужный во всех проектах, достаточно задать в машинном.
VARS="$(jq -n \
  --argjson user_secrets "$(parse_secrets_file "$USER_SECRETS")" \
  --argjson secrets "$(parse_secrets_file "$SECRETS_FILE")" \
  --arg repo_root "$REPO_ROOT" \
  '$ENV + $user_secrets + $secrets + {REPO_ROOT: $repo_root}')"

# ── 3. Слить слои, раскрыть подстановки ──────────────────────
# x-requires пока НЕ вырезаем: гейт ниже смотрит на него уже раскрытым.
LAYER_PLAT="$(read_layer "$PLATFORM_MCP")"
LAYER_USER="$(read_layer "$USER_MCP")"
LAYER_PROJ="$(read_layer "$PROJECT_MCP")"
LAYER_LOCAL="$(read_layer "$LOCAL_MCP")"

MERGED_RAW="$(jq -n \
  --argjson plat "$LAYER_PLAT" \
  --argjson user "$LAYER_USER" \
  --argjson proj "$LAYER_PROJ" \
  --argjson local "$LAYER_LOCAL" \
  --argjson vars "$VARS" '
  def strip_meta: with_entries(select(.key | startswith("//") | not));
  def expand($v): walk(
    if type == "string" then
      # неизвестное имя оставляем как есть — это диагностируемо, в отличие от пустой строки
      gsub("\\$\\{(?<k>[A-Za-z_][A-Za-z0-9_]*)\\}"; ($v[.k] // ("${" + .k + "}")))
    else . end);

  (($plat.mcpServers // {})
   * ($user.mcpServers // {})
   * ($proj.mcpServers // {})
   * ($local.mcpServers // {}))
  | with_entries(select(.value != null))          # null сверху = выключить нижний
  | with_entries(.value |= strip_meta)             # вырезать //-комментарии
  | expand($vars)
')"

# Провенанс: в каком слое сервер объявлен и какие слои он перекрывает. Нужен
# не раздаче, а `adc mcp list` — без него вывод показывает ЧТО роздано, но не
# отвечает на вопрос «а почему у меня тут это и где мне это править».
ORIGINS="$(jq -n \
  --argjson plat "$LAYER_PLAT" \
  --argjson user "$LAYER_USER" \
  --argjson proj "$LAYER_PROJ" \
  --argjson local "$LAYER_LOCAL" '
  def names: (.mcpServers // {}) | keys;
  # снизу вверх; последний, где имя встретилось, и есть слой-владелец
  [ ($plat  | names | map({name: ., scope: "global"})),
    ($user  | names | map({name: ., scope: "user"})),
    ($proj  | names | map({name: ., scope: "project"})),
    ($local | names | map({name: ., scope: "local"})) ]
  | flatten
  | group_by(.name)
  | map({ key: .[0].name,
          value: { scope: (last | .scope), shadows: (map(.scope) | .[0:-1]) } })
  | from_entries
')"

# ── 4. Гейт по x-requires ────────────────────────────────────
# Причины отсева копим не только в лог, но и в JSON: их печатает `adc mcp list`
# ("○ figma — нет FIGMA_MCP_ENABLED"), а `adc mcp enable` по полю need_env
# знает, что именно спросить у человека. Один источник правды: список из
# list'а не может разойтись с тем, что раздача реально сделала.
KEEP=""
REASONS='{}'
for name in $(echo "$MERGED_RAW" | jq -r 'keys[]'); do
  ok=1 why="" need_env="" need_path=""
  while IFS= read -r req; do
    [ -n "$req" ] || continue
    case "$req" in
      env:*)
        var="${req#env:}"
        val="$(echo "$VARS" | jq -r --arg k "$var" '.[$k] // ""')"
        [ -n "$val" ] || { ok=0; why="нет переменной $var"; need_env="$var"; }
        ;;
      path:*)
        p="${req#path:}"
        [ -e "$p" ] || { ok=0; why="нет пути $p"; need_path="$p"; }
        ;;
      *) warn "  сервер '$name': непонятное x-requires «$req» — игнорирую" ;;
    esac
    [ "$ok" = 1 ] || break
  done < <(echo "$MERGED_RAW" | jq -r --arg n "$name" '.[$n]["x-requires"] // [] | .[]')

  if [ "$ok" = 1 ]; then
    KEEP="$KEEP $name"
  else
    REASONS="$(jq -n --argjson base "$REASONS" --arg n "$name" --arg w "$why" \
      --arg e "$need_env" --arg p "$need_path" \
      '$base + {($n): {reason: $w, need_env: (if $e == "" then null else $e end),
                       need_path: (if $p == "" then null else $p end)}}')"
    dim "  ~ $name не раздаю: $why"
  fi
done
KEEP="${KEEP# }"

# Сервер, выключенный через `null` в вышележащем слое, до гейта не доходит
# вовсе — его отфильтровал мердж. Для list'а это всё равно надо показать:
# «его нет» и «его выключили вот здесь» — разные ответы.
DISABLED="$(jq -n \
  --argjson plat "$LAYER_PLAT" --argjson user "$LAYER_USER" \
  --argjson proj "$LAYER_PROJ" --argjson local "$LAYER_LOCAL" '
  def offs($s): (.mcpServers // {}) | to_entries
                | map(select(.value == null) | {key: .key, value: $s}) ;
  ($plat | offs("global")) + ($user | offs("user"))
  + ($proj | offs("project")) + ($local | offs("local"))
  | from_entries')"

# Имена OAuth-серверов забираем ДО вырезания метки: ниже по ним решается,
# добавлять ли сервер в Codex.
OAUTH_NAMES="$(echo "$MERGED_RAW" | jq -r 'to_entries[] | select(.value["x-oauth"] == true) | .key' | tr '\n' ' ')"

MERGED="$(echo "$MERGED_RAW" | jq --argjson keep "$(printf '%s\n' $KEEP | jq -R . | jq -sc .)" '
  with_entries(select(.key as $k | $keep | index($k)))
  | with_entries(.value |= del(.["x-requires"], .["x-oauth"]))
')"

# Пробелы, а не переводы строк: ниже имена ищутся подстрокой в " $NAMES ",
# и с \n в разделителе совпадение молча не находится — сервер каждый раз
# считался бы устаревшим, снимался и ставился заново.
NAMES="$(echo "$MERGED" | jq -r 'keys[]' | tr '\n' ' ')"
NAMES="${NAMES% }"
COUNT="$(echo "$MERGED" | jq -r 'length')"

# Незакрытая подстановка — почти всегда забытый x-requires. Не падаем (сервер
# может быть и рабочим), но говорим: иначе агент получит буквальное "${TOKEN}".
UNRESOLVED="$(echo "$MERGED" | jq -r '[paths(type=="string") as $p | getpath($p) | select(test("\\$\\{"))] | unique | join(", ")')"
[ -n "$UNRESOLVED" ] && warn "нераскрытые подстановки: $UNRESOLVED (добавь значение в $SECRETS_FILE или отсеки сервер через x-requires)"

# ── 4a. Режим плана: отдать посчитанное и выйти, ничего не записав ──
# Секретов в выводе нет: серверы отдаём «как объявлены в слое», ДО подстановки
# значений. Иначе `adc mcp list` показывал бы токен на экране, а его вывод
# уходит в чужие пасты и в логи. Кто активен и почему — считается по уже
# раскрытым значениям, так что правда не теряется.
if [ "$MODE" = plan ]; then
  jq -n \
    --arg repo "$REPO_ROOT" \
    --arg f_global "$PLATFORM_MCP" --arg f_user "$USER_MCP" \
    --arg f_project "$PROJECT_MCP" --arg f_local "$LOCAL_MCP" \
    --arg s_user "$USER_SECRETS" --arg s_project "$SECRETS_FILE" \
    --argjson plat "$LAYER_PLAT" --argjson user "$LAYER_USER" \
    --argjson proj "$LAYER_PROJ" --argjson local "$LAYER_LOCAL" \
    --argjson origins "$ORIGINS" --argjson reasons "$REASONS" \
    --argjson disabled "$DISABLED" \
    --argjson active "$(printf '%s\n' $NAMES | jq -R . | jq -sc 'map(select(. != ""))')" \
    --argjson oauth "$(printf '%s\n' $OAUTH_NAMES | jq -R . | jq -sc 'map(select(. != ""))')" \
    --argjson e_global "$([ -f "$PLATFORM_MCP" ] && echo true || echo false)" \
    --argjson e_user "$([ -f "$USER_MCP" ] && echo true || echo false)" \
    --argjson e_project "$([ -f "$PROJECT_MCP" ] && echo true || echo false)" \
    --argjson e_local "$([ -f "$LOCAL_MCP" ] && echo true || echo false)" '
    def raw($layer): ($layer.mcpServers // {}) | with_entries(select(.value != null))
                     | with_entries(.value |= with_entries(select(.key | startswith("//") | not)));
    # defs — слитое определение КАЖДОГО сервера до подстановки значений.
    # Именно его показывает list: слой-владелец может нести лишь кусок
    # переопределения (один env поверх платформенных command/args), и рисовать
    # по нему командную строку значило бы показывать пустую строку вместо
    # реальной. Подстановки здесь не раскрыты — токенов на экране нет.
    (raw($plat) * raw($user) * raw($proj) * raw($local)) as $defs
    | { repo_root: $repo, defs: $defs,
      layers: [
        {scope: "global",  file: $f_global,  exists: $e_global,  servers: raw($plat)},
        {scope: "user",    file: $f_user,    exists: $e_user,    servers: raw($user)},
        {scope: "project", file: $f_project, exists: $e_project, servers: raw($proj)},
        {scope: "local",   file: $f_local,   exists: $e_local,   servers: raw($local)}
      ],
      secrets: {user: $s_user, project: $s_project},
      origins: $origins, reasons: $reasons, disabled: $disabled,
      active: $active, oauth: $oauth }'
  exit 0
fi

# ── 5. Что чистить: разложенное в прошлый раз минус нужное сейчас ──
PREV=""
[ -f "$STATE_FILE" ] && PREV="$(cat "$STATE_FILE")"
STALE=""
for prev in $PREV; do
  case " $NAMES " in *" $prev "*) ;; *) STALE="$STALE $prev";; esac
done

# ── 6. Claude Code — <repo>/.mcp.json ────────────────────────
# Если .mcp.json уже лежит, а нашего state-файла нет, значит файл написал
# проект руками — не трогаем, иначе молча снесём чужую настройку.
if [ -e "$CLAUDE_MCP" ] && [ ! -f "$STATE_FILE" ]; then
  warn "$CLAUDE_MCP существует, но раскладывали его не мы — оставляю как есть"
  warn "  (убери файл, если хочешь получать MCP из платформы)"
elif [ "$COUNT" = 0 ]; then
  rm -f "$CLAUDE_MCP"
else
  mkdir -p "$(dirname "$CLAUDE_MCP")"
  # 600 сразу, ДО записи: в файле могут быть раскрытые токены.
  touch "$CLAUDE_MCP"; chmod 600 "$CLAUDE_MCP"
  jq -n --argjson s "$MERGED" '{mcpServers: $s}' > "$CLAUDE_MCP"
  dim "  Claude: $CLAUDE_MCP"
fi

# ── 7. Codex — ~/.codex/config.toml через собственный CLI ────
# `codex mcp add` перезаписывает запись с тем же именем, так что идемпотентно.
if command -v codex >/dev/null 2>&1; then
  for name in $STALE; do
    codex mcp remove "$name" >/dev/null 2>&1 && dim "  - Codex: убрал $name" || true
  done
  CODEX_CONFIG="${CODEX_HOME:-$HOME/.codex}/config.toml"

  # Что лежит в config.toml под этим именем: none | headers | other.
  # Нужно ровно для серверов с заголовками. `other` — либо наш прошлый выхлоп
  # (устаревший stdio-сервер, либо url с токеном в query), либо запись, которая
  # без заголовков всё равно не заработает: такую снимаем. `headers` — человек
  # вписал руками по нашей же подсказке, это трогать нельзя.
  #
  # Не парсер произвольного TOML, а точечный awk по тому единственному
  # формату, который сюда пишет `codex mcp add` или наша же подсказка
  # человеку: секция `[mcp_servers.NAME]` (с учётом кавычек), внутри неё до
  # следующей top-level `[` — вхождение `http_headers` (инлайн-таблица или
  # под-секция `[mcp_servers.NAME.http_headers]`, обе матчатся одной проверкой).
  codex_entry_kind() {
    local path="$1" name="$2"
    [ -f "$path" ] || { echo none; return 0; }
    awk -v name="$name" '
      /^\[/ {
        line = $0
        gsub(/^\[|\]$/, "", line)
        is_ours = (line == "mcp_servers." name) || (line == "mcp_servers.\"" name "\"") \
                  || (line ~ ("^mcp_servers\\.\"?" name "\"?\\."))
        if (is_ours) {
          in_section = 1; found = 1
          if (line ~ /http_headers/) has_headers = 1   # сама подсекция [...http_headers]
          next
        }
        if (in_section) exit                            # вышли из нужной секции
        next
      }
      in_section && /http_headers/ { has_headers = 1 }   # инлайн-таблица внутри секции
      END {
        if (!found) print "none"
        else if (has_headers) print "headers"
        else print "other"
      }
    ' "$path"
  }

  codex_n=0
  for name in $NAMES; do
    url="$(echo "$MERGED" | jq -r --arg n "$name" '.[$n].url // ""')"
    if [ -n "$url" ]; then
      case " $OAUTH_NAMES " in
        *" $name "*)
          # Не добавляем автоматически — см. блок про x-oauth в шапке.
          dim "  Codex: OAuth-сервер '$name' не добавляю автоматически (логин ручной)"
          dim "    хочешь его в Codex — добавь сам, когда готов авторизоваться:"
          dim "    codex mcp add $name --url $url"
          continue ;;
      esac
      # Заголовки CLI не выставляет — печатаем готовый блок для config.toml.
      # Значения заголовков маскируем: в них токен, а вывод уходит в лог
      # postCreate и в скроллбек терминала.
      if [ "$(echo "$MERGED" | jq -r --arg n "$name" '.[$n].headers // {} | length')" != 0 ]; then
        case "$(codex_entry_kind "$CODEX_CONFIG" "$name")" in
          headers)
            dim "  Codex: '$name' уже вписан руками (есть http_headers) — не трогаю"
            codex_n=$((codex_n + 1)); continue ;;
          other)
            codex mcp remove "$name" >/dev/null 2>&1 \
              && dim "  - Codex: снял запись '$name' от прошлой раздачи (без заголовков не заработала бы)" || true ;;
        esac
        dim "  Codex: '$name' не добавляю — заголовки через \`codex mcp add\` не выставляются"
        dim "    хочешь его в Codex — впиши руками в ${CODEX_HOME:-\$HOME/.codex}/config.toml,"
        dim "    подставив значения из .agents/mcp.secrets.env:"
        dim "      [mcp_servers.$name]"
        dim "      url = \"$url\""
        dim "      [mcp_servers.$name.http_headers]"
        while IFS= read -r h; do
          [ -n "$h" ] && dim "      $h"
        done < <(echo "$MERGED" | jq -r --arg n "$name" \
          '.[$n].headers // {} | to_entries[] | "\(.key) = \"<значение из secrets>\""')
        continue
      fi

      # Обычный streamable-HTTP: авторизация внутри url либо не нужна вовсе.
      if codex mcp add "$name" --url "$url" >/dev/null; then
        codex_n=$((codex_n + 1))
      else
        warn "  Codex: не смог добавить $name"
      fi
      continue
    fi

    # env → повторяемые --env K=V; command и args — после `--`
    env_args=()
    while IFS= read -r kv; do
      [ -n "$kv" ] && env_args+=(--env "$kv")
    done < <(echo "$MERGED" | jq -r --arg n "$name" '.[$n].env // {} | to_entries[] | "\(.key)=\(.value)"')

    cmd_args=()
    while IFS= read -r a; do
      cmd_args+=("$a")
    done < <(echo "$MERGED" | jq -r --arg n "$name" '[.[$n].command] + (.[$n].args // []) | .[]')

    if [ "${#cmd_args[@]}" -eq 0 ] || [ -z "${cmd_args[0]}" ]; then
      warn "  Codex: у сервера '$name' нет ни command, ни url — пропускаю"
      continue
    fi
    if codex mcp add "$name" "${env_args[@]}" -- "${cmd_args[@]}" >/dev/null; then
      codex_n=$((codex_n + 1))
    else
      warn "  Codex: не смог добавить $name"
    fi
  done
  chmod 600 "${CODEX_HOME:-$HOME/.codex}/config.toml" 2>/dev/null || true
  # Считаем по факту добавленного: OAuth-серверы сюда осознанно не попали.
  dim "  Codex: $codex_n из $COUNT сервер(ов) в ${CODEX_HOME:-$HOME/.codex}/config.toml (нужен рестарт codex)"
else
  dim "  Codex не в PATH — пропускаю"
fi

# ── 8. Hermes — ~/.hermes/config.yaml, ключ mcp_servers ──────
# `yq` — имя, под которым живут два несовместимых проекта: kislyuk/yq
# (python, обёртка над jq — `apt install yq` на Debian/Ubuntu, синтаксис
# `-y -i "<jq-фильтр>"`) и mikefarah/yq (Go — `brew install yq`, свой язык
# выражений, флага `-y` нет вовсе, на нём падает разбор аргументов). Который
# достанется — решает не наш Dockerfile (там нужный, apt-шный), а хостовое
# окружение, где вообще-то тоже гоняется этот скрипт (см. шапку файла), и там
# уже как повезёт. Поэтому не полагаемся на язык выражений yq вообще — он тут
# только конвертер формата (YAML<->JSON, стабильно у обоих), а мерж и del —
# нашим jq, который и так жёсткая зависимость. Комментарии в конфиге теряются
# при таком раунд-трипе — так было и раньше, kislyuk-вариант делает то же
# самое внутри себя.
yq_flavor() {
  case "$(yq --version 2>&1)" in
    *mikefarah*) echo go ;;
    *) echo python ;;
  esac
}
yq_to_json() { # $1 = yaml-файл -> JSON на stdout
  if [ "$(yq_flavor)" = go ]; then yq -o=json e '.' "$1"; else yq . "$1"; fi
}
yq_from_json_inplace() { # stdin = JSON -> перезаписывает $1 как YAML
  if [ "$(yq_flavor)" = go ]; then yq -P -o=yaml -p=json e '.' - > "$1"; else yq -y . > "$1"; fi
}

if command -v yq >/dev/null 2>&1 && [ -d "$(dirname "$HERMES_CONFIG")" ]; then
  [ -s "$HERMES_CONFIG" ] || echo "{}" > "$HERMES_CONFIG"
  for name in $STALE; do
    CUR_JSON="$(yq_to_json "$HERMES_CONFIG" 2>/dev/null)" || CUR_JSON=""
    if [ -n "$CUR_JSON" ] \
      && NEW_JSON="$(echo "$CUR_JSON" | jq -c --arg name "$name" 'del(.mcp_servers[$name])')" \
      && echo "$NEW_JSON" | yq_from_json_inplace "$HERMES_CONFIG"; then
      dim "  - Hermes: убрал $name"
    fi
  done
  if [ "$COUNT" != 0 ]; then
    CUR_JSON="$(yq_to_json "$HERMES_CONFIG" 2>/dev/null)" || CUR_JSON=""
    if [ -n "$CUR_JSON" ] \
      && NEW_JSON="$(echo "$CUR_JSON" | jq -c --argjson merged "$MERGED" '.mcp_servers = ((.mcp_servers // {}) + $merged)')" \
      && echo "$NEW_JSON" | yq_from_json_inplace "$HERMES_CONFIG"; then
      chmod 600 "$HERMES_CONFIG" 2>/dev/null || true
      dim "  Hermes: $COUNT сервер(ов) в $HERMES_CONFIG"
    else
      warn "  Hermes: не смог записать mcp_servers в $HERMES_CONFIG"
    fi
  fi
else
  dim "  Hermes-конфига нет (или нет yq) — пропускаю"
fi

# ── 9. DSH — ~/.dsh/cordis.patch.yml, patch-op `insert` ──────
# Проектного скоупа у DSH нет вовсе: MCP настраивается патчем композиции в
# home. В devcontainer'е ~/.dsh — пер-проектный маунт, так что home здесь и
# есть проектный скоуп (тот же приём, что с ~/.codex и ~/.hermes).
#
# Файл общий с человеком: свой блок держим в маркерах и перезаписываем только
# его. Всё, что человек допишет выше или ниже, переживает раздачу.
#
# YAML строим через jq (JSON — подмножество YAML 1.2, `tojson` в jq не
# эскейпит не-ASCII — тот же эффект, что был у json.dumps(ensure_ascii=False))
# + bash/awk для управления маркерным блоком в существующем файле. Без
# pyyaml-страховки перед записью (была опциональна и раньше) — компромисс,
# см. HANDOFF/план: формат генерируем строго сами, тесты — новая страховка.
# Каталог заводим сами, если dsh установлен: в devcontainer'е ~/.dsh есть
# всегда (предсоздан в образе + маунт), а на голом хосте он появляется
# только после первого запуска агента — и первая раздача уходила бы в никуда.
if command -v dsh >/dev/null 2>&1 && [ ! -d "$DSH_HOME_DIR" ]; then
  mkdir -p "$DSH_HOME_DIR"
fi
if [ -d "$DSH_HOME_DIR" ]; then
  DSH_START="# ai-devcontainer:mcp:start"
  DSH_END="# ai-devcontainer:mcp:end"

  DSH_ITEMS="$(echo "$MERGED" | jq -c \
    --argjson oauth "$(printf '%s\n' $OAUTH_NAMES | jq -R . | jq -sc 'map(select(length > 0))')" \
    --arg repo_root "$REPO_ROOT" '
    def valid_name: test("^[A-Za-z0-9_-]{1,32}$");
    to_entries
    | sort_by(.key)
    | map(
        . as $e |
        # serverName у DSH — неймспейс имён тулов, шаблон жёсткий. Наши
        # стоковые ему соответствуют; проектный сервер может назваться как угодно.
        if ($e.key | valid_name | not) then
          {name: $e.key, status: "skip", reason: "имя не подходит под [A-Za-z0-9_-]{1,32}"}
        elif ($oauth | index($e.key)) then
          # Ровно та же политика, что в Codex: логин проходит человек, а
          # OAuth-сервер в конфиге просит авторизацию при каждом старте агента.
          {name: $e.key, status: "skip", reason: "OAuth — добавь сам, когда готов авторизоваться"}
        elif ($e.value.url) then
          {name: $e.key, status: "ok", cfg: (
            {serverName: $e.key, transport: "streamable-http", url: $e.value.url}
            + (if (($e.value.headers // {}) | length) > 0 then {headers: $e.value.headers} else {} end)
          )}
        elif ($e.value.command) then
          {name: $e.key, status: "ok", cfg: (
            {serverName: $e.key, transport: "stdio", command: $e.value.command, args: ($e.value.args // [])}
            + (if (($e.value.env // {}) | length) > 0 then {env: $e.value.env} else {} end)
            # cwd задаём явно: dsh стартует сервер из своего рабочего каталога,
            # а он у агента не обязан совпадать с корнем репозитория.
            + {cwd: $repo_root}
          )}
        else
          {name: $e.key, status: "skip", reason: "нет ни command, ни url"}
        end
      )
    | .[]
  ')"

  DSH_LINES=()
  dsh_count=0
  while IFS= read -r item; do
    [ -z "$item" ] && continue
    status="$(echo "$item" | jq -r '.status')"
    name="$(echo "$item" | jq -r '.name')"
    if [ "$status" = "skip" ]; then
      reason="$(echo "$item" | jq -r '.reason')"
      warn "  DSH: сервер '$name' не раздаю: $reason"
      continue
    fi
    DSH_LINES+=("- insert:")
    DSH_LINES+=("    - id: \"ai-devcontainer-mcp-$name\"")
    DSH_LINES+=('      name: "@deepseek-ai/dsh-mcp-client"')
    DSH_LINES+=("      config:")
    while IFS= read -r kv_line; do
      DSH_LINES+=("        $kv_line")
    done < <(echo "$item" | jq -r '.cfg | to_entries[] | "\(.key): \(.value | tojson)"')
    dsh_count=$((dsh_count + 1))
  done <<<"$DSH_ITEMS"

  # Перезаписываем только свой участок; чужие патчи в файле не трогаем.
  DSH_KEPT=()
  if [ -f "$DSH_PATCH" ]; then
    inside=0
    while IFS= read -r line || [ -n "$line" ]; do
      if [ "${line:0:${#DSH_START}}" = "$DSH_START" ]; then
        inside=1; continue
      fi
      if [ "$inside" = 1 ]; then
        [ "${line:0:${#DSH_END}}" = "$DSH_END" ] && inside=0
        continue
      fi
      DSH_KEPT+=("$line")
    done < "$DSH_PATCH"
  fi
  while [ "${#DSH_KEPT[@]}" -gt 0 ] && [ -z "${DSH_KEPT[-1]}" ]; do
    unset 'DSH_KEPT[-1]'
  done

  # Пустой патч-лист dsh записывает как `[]`, и это единственная форма, к
  # которой наши элементы дописать НЕЛЬЗЯ: `[]` + `- insert:` — не «список из
  # одного», а сломанный документ, и агент с ним не поднимется вовсе.
  # Плейсхолдер снимаем; всё остальное чужое остаётся как есть.
  if [ "${#DSH_LINES[@]}" -gt 0 ] && [ "${#DSH_KEPT[@]}" -gt 0 ]; then
    only_bracket=1 non_comment=0
    for l in "${DSH_KEPT[@]}"; do
      trimmed="$(printf '%s' "$l" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
      [ -z "$trimmed" ] && continue
      case "$trimmed" in '#'*) continue ;; esac
      non_comment=$((non_comment + 1))
      [ "$trimmed" = "[]" ] || only_bracket=0
    done
    if [ "$non_comment" -gt 0 ] && [ "$only_bracket" = 1 ]; then
      NEW_KEPT=()
      for l in "${DSH_KEPT[@]}"; do
        trimmed="$(printf '%s' "$l" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
        [ "$trimmed" = "[]" ] || NEW_KEPT+=("$l")
      done
      DSH_KEPT=("${NEW_KEPT[@]+"${NEW_KEPT[@]}"}")
      while [ "${#DSH_KEPT[@]}" -gt 0 ] && [ -z "${DSH_KEPT[-1]}" ]; do
        unset 'DSH_KEPT[-1]'
      done
    fi
  fi

  if {
    for l in "${DSH_KEPT[@]+"${DSH_KEPT[@]}"}"; do printf '%s\n' "$l"; done
    if [ "${#DSH_KEPT[@]}" -gt 0 ] && [ "${#DSH_LINES[@]}" -gt 0 ]; then printf '\n'; fi
    if [ "${#DSH_LINES[@]}" -gt 0 ]; then
      printf '%s\n' "$DSH_START — генерируется tooling/wire-mcp.sh, правки затрутся"
      for l in "${DSH_LINES[@]}"; do printf '%s\n' "$l"; done
      printf '%s\n' "$DSH_END"
    fi
  } > "$DSH_PATCH.new" && mv "$DSH_PATCH.new" "$DSH_PATCH" && chmod 600 "$DSH_PATCH"; then
    dim "  DSH: $dsh_count сервер(ов) в $DSH_PATCH"
  else
    warn "  DSH: не смог записать $DSH_PATCH"
    rm -f "$DSH_PATCH.new"
  fi
else
  dim "  DSH: нет $DSH_HOME_DIR и dsh не в PATH — пропускаю"
fi

# ── 10. Запомнить, что разложили ─────────────────────────────
mkdir -p "$(dirname "$STATE_FILE")"
echo "$NAMES" > "$STATE_FILE"

# ── 11. Разовая правка .gitignore проекта ────────────────────
# Генерируемое и секретное в гите не место. Дописываем один раз (как ссылку на
# индекс скиллов в AGENTS.md) и дальше файл не трогаем.
GITIGNORE="$REPO_ROOT/.gitignore"
if [ -f "$GITIGNORE" ] && ! grep -qF "/.mcp.json" "$GITIGNORE"; then
  cat >> "$GITIGNORE" <<'IGN'

# MCP-серверы агентов — раздаются платформой (tooling/wire-mcp.sh) из четырёх
# слоёв, зависят от версии платформы. Серверы команды — в .agents/mcp.json,
# он как раз В ГИТЕ. Локальный слой (.agents/mcp.local.json) — только мой.
# В .mcp.json попадают РАСКРЫТЫЕ секреты — в гит его нельзя тем более.
/.mcp.json
/.agents/mcp.secrets.env
/.agents/mcp.local.json
# Выхлоп браузерного MCP: скриншоты, трейсы, скачанные файлы.
.playwright-mcp/
IGN
  log ".gitignore: добавил /.mcp.json, секреты, локальный слой и .playwright-mcp/ (разовая правка)"
fi

# Страховка на случай, если .gitignore правился до появления секретов.
if [ -f "$GITIGNORE" ] && ! grep -qF "mcp.secrets.env" "$GITIGNORE"; then
  printf '\n# Секреты MCP-серверов проекта (токены). Только локально.\n/.agents/mcp.secrets.env\n' >> "$GITIGNORE"
  log ".gitignore: добавил /.agents/mcp.secrets.env"
fi

# То же для локального слоя: он появился позже .mcp.json, и в проектах,
# заведённых раньше, блок выше уже дописан — новая строка туда не попала бы,
# а личный сервер уехал бы в гит команды.
if [ -f "$GITIGNORE" ] && ! grep -qF "mcp.local.json" "$GITIGNORE"; then
  printf '\n# Локальный слой MCP: мои серверы только в этом проекте.\n/.agents/mcp.local.json\n' >> "$GITIGNORE"
  log ".gitignore: добавил /.agents/mcp.local.json"
fi

[ -f "$SECRETS_FILE" ] && chmod 600 "$SECRETS_FILE" 2>/dev/null || true

log "MCP: $COUNT сервер(ов) → Claude + Codex + Hermes + DSH${STALE:+ (убрано:$STALE)}"
