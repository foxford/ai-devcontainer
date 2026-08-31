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

PLATFORM_MCP="${AI_DEVCONTAINER_MCP:-$PLATFORM_ROOT/mcp/servers.json}"
PROJECT_MCP="$REPO_ROOT/.agents/mcp.json"
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

command -v jq >/dev/null 2>&1 || { warn "нет jq — MCP не раздаю"; exit 0; }

# Репозиторий платформы, открытый сам в себе — не раздаём, и это не лень.
# У платформы `.agents` — симлинк в `skeleton/.agents`, то есть «проектным
# слоем» тут оказался бы шаблон новых проектов: любое локальное перекрытие
# уехало бы во ВСЕ проекты, созданные дальше. Разводить эти два смысла дороже,
# чем обойтись без MCP при разработке самой платформы.
if [ "$(readlink -f "$REPO_ROOT")" = "$(readlink -f "$PLATFORM_ROOT")" ]; then
  dim "  это репозиторий платформы — MCP не раздаю"
  exit 0
fi

# ── 1. Валидация слоёв ───────────────────────────────────────
# Именно здесь, в основном шелле: ниже слои читаются в подстановке команд, а
# `exit` внутри неё убивает только подоболочку — скрипт поехал бы дальше с
# пустым вводом и неразборчивым «invalid JSON text passed to --argjson».
for layer in "$PLATFORM_MCP" "$PROJECT_MCP"; do
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
if [ -L "$SECRETS_FILE" ]; then
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

VARS="$(jq -n \
  --argjson secrets "$(parse_secrets_file "$SECRETS_FILE")" \
  --arg repo_root "$REPO_ROOT" \
  '$ENV + $secrets + {REPO_ROOT: $repo_root}')"

# ── 3. Слить слои, раскрыть подстановки ──────────────────────
# x-requires пока НЕ вырезаем: гейт ниже смотрит на него уже раскрытым.
MERGED_RAW="$(jq -n \
  --argjson plat "$(read_layer "$PLATFORM_MCP")" \
  --argjson proj "$(read_layer "$PROJECT_MCP")" \
  --argjson vars "$VARS" '
  def strip_meta: with_entries(select(.key | startswith("//") | not));
  def expand($v): walk(
    if type == "string" then
      # неизвестное имя оставляем как есть — это диагностируемо, в отличие от пустой строки
      gsub("\\$\\{(?<k>[A-Za-z_][A-Za-z0-9_]*)\\}"; ($v[.k] // ("${" + .k + "}")))
    else . end);

  (($plat.mcpServers // {}) * ($proj.mcpServers // {}))
  | with_entries(select(.value != null))          # null у проекта = выключить
  | with_entries(.value |= strip_meta)             # вырезать //-комментарии
  | expand($vars)
')"

# ── 4. Гейт по x-requires ────────────────────────────────────
KEEP=""
for name in $(echo "$MERGED_RAW" | jq -r 'keys[]'); do
  ok=1 why=""
  while IFS= read -r req; do
    [ -n "$req" ] || continue
    case "$req" in
      env:*)
        var="${req#env:}"
        val="$(echo "$VARS" | jq -r --arg k "$var" '.[$k] // ""')"
        [ -n "$val" ] || { ok=0; why="нет переменной $var"; }
        ;;
      path:*)
        p="${req#path:}"
        [ -e "$p" ] || { ok=0; why="нет пути $p"; }
        ;;
      *) warn "  сервер '$name': непонятное x-requires «$req» — игнорирую" ;;
    esac
    [ "$ok" = 1 ] || break
  done < <(echo "$MERGED_RAW" | jq -r --arg n "$name" '.[$n]["x-requires"] // [] | .[]')

  if [ "$ok" = 1 ]; then
    KEEP="$KEEP $name"
  else
    dim "  ~ $name не раздаю: $why"
    # Сказать «нет переменной» мало: человек в этот момент как раз и хочет
    # знать, ГДЕ она задаётся. Печатаем один раз на прогон, чтобы список
    # отсечённых серверов не превращался в простыню.
    case "$why" in
      "нет переменной"*)
        if [ -z "${SECRETS_HINT_SHOWN:-}" ]; then
          SECRETS_HINT_SHOWN=1
          dim "     переменные — в .agents/mcp.secrets.env (KEY=VALUE), образец рядом: mcp.secrets.env.example"
        fi ;;
    esac
  fi
done
KEEP="${KEEP# }"

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
# Пишем напрямую (см. шапку). yq здесь питоновский (kislyuk/yq) — тот же, что
# использует .hermes/bootstrap.sh, выражение внутри обычный jq.
if command -v yq >/dev/null 2>&1 && [ -d "$(dirname "$HERMES_CONFIG")" ]; then
  [ -s "$HERMES_CONFIG" ] || echo "{}" > "$HERMES_CONFIG"
  for name in $STALE; do
    yq -y -i "del(.mcp_servers[\"$name\"])" "$HERMES_CONFIG" 2>/dev/null \
      && dim "  - Hermes: убрал $name" || true
  done
  if [ "$COUNT" != 0 ]; then
    if yq -y -i ".mcp_servers = ((.mcp_servers // {}) + $MERGED)" "$HERMES_CONFIG"; then
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

# MCP-серверы агентов — раздаются платформой (tooling/wire-mcp.sh) из двух
# слоёв, зависят от версии платформы. Проектные отличия — в .agents/mcp.json.
# В .mcp.json попадают РАСКРЫТЫЕ секреты — в гит его нельзя тем более.
/.mcp.json
/.agents/mcp.secrets.env
# Выхлоп браузерного MCP: скриншоты, трейсы, скачанные файлы.
.playwright-mcp/
IGN
  log ".gitignore: добавил /.mcp.json, секреты и .playwright-mcp/ (разовая правка)"
fi

# Страховка на случай, если .gitignore правился до появления секретов.
if [ -f "$GITIGNORE" ] && ! grep -qF "mcp.secrets.env" "$GITIGNORE"; then
  printf '\n# Секреты MCP-серверов проекта (токены). Только локально.\n/.agents/mcp.secrets.env\n' >> "$GITIGNORE"
  log ".gitignore: добавил /.agents/mcp.secrets.env"
fi

[ -f "$SECRETS_FILE" ] && chmod 600 "$SECRETS_FILE" 2>/dev/null || true

log "MCP: $COUNT сервер(ов) → Claude + Codex + Hermes + DSH${STALE:+ (убрано:$STALE)}"
