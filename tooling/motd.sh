#!/bin/sh
# motd.sh — приветствие контейнера. Живёт в МОНТИРУЕМОМ tooling/, а не в образе:
# правка текста видна в новом шелле сразу, без пересборки dev-base.
#
# Зовётся из ~/.zshrc (запечён в образ) — тот только ищет этот файл и запускает
# его через sh. Поэтому здесь POSIX sh и никаких зависимостей.
#
# $PROJECT_NAME прокидывает вызывающий шелл.

NAME="${PROJECT_NAME:-dev}"

# Страховка переписки Claude: не чаще раза в сутки, в фоне и молча — приглашение
# ждать не должно. Сам скрипт решает, нужен ли снапшот (claude-snapshot daily).
if command -v claude-snapshot >/dev/null 2>&1; then
  (claude-snapshot daily >/dev/null 2>&1 &) 2>/dev/null
fi

echo "🤖 ${NAME} Dev Container"
echo ""
echo "  📦 Dev    pnpm install · pnpm -r build · pnpm test · pnpm lint"
echo "  🤝 AI     claude · opencode · codex · hermes · dsh"
echo "  🕸  Граф   graphify-view             граф кодовой базы в браузере"
echo "            graphify update .         пересобрать после правок"
echo "            graphify query <вопрос>   спросить граф: что с чем связано"
echo "            graphify-label-paths      осмысленные имена сообществ по путям"
echo "            graphify export obsidian  выгрузить как Obsidian-vault"
echo "            /graphify --update        обновить через AI-агента (имена + доки)"
echo "            pnpm nx graph             граф зависимостей пакетов (Nx)"
echo "  🛠  Платформа adc sync      применить к проекту + что приехало"
echo "               ... doctor            сверить окружение и devcontainer"
echo "  🧩 Скиллы adc skill list   что откуда приезжает"
echo "            ... fork <скилл> [файл]   править скилл под проект"
echo "            ... status                разошлась ли платформа под форками"
echo "  🔧 Утилиты pnpm-patch-dep · git"

# Проектное дополнение: у проекта могут быть свои команды, о которых платформа
# не знает (свои графы, генераторы, туннели). Тот же принцип, что со скиллами и
# доками — платформа даёт общее, проект дополняет, а не переписывает.
# Корень репо ищем вверх от текущего каталога: терминал может открыться в
# подкаталоге, и тогда $PWD — не корень.
DIR="${REPO_ROOT:-$PWD}"
while [ "$DIR" != "/" ]; do
  if [ -r "$DIR/.devcontainer/motd.local.sh" ]; then
    sh "$DIR/.devcontainer/motd.local.sh"
    break
  fi
  DIR="$(dirname "$DIR")"
done

# Платформа уехала вперёд проекта — сказать об этом. Иначе о новых скиллах,
# доках и правилах узнать неоткуда: они лежат в клоне платформы и ждут sync,
# никак себя не проявляя. Отметку кладёт `adc sync`.
REPO="${REPO_ROOT:-$PWD}"
while [ "$REPO" != "/" ] && [ ! -d "$REPO/.git" ] && [ ! -d "$REPO/.devcontainer" ]; do
  REPO="$(dirname "$REPO")"
done
PLAT="${AI_DEVCONTAINER_HOME:-/opt/ai-devcontainer}"
if [ -f "$REPO/.claude/.platform-rev" ] && [ -d "$PLAT/.git" ]; then
  APPLIED="$(cat "$REPO/.claude/.platform-rev" 2>/dev/null)"
  CURRENT="$(git -C "$PLAT" rev-parse --short HEAD 2>/dev/null)"
  if [ -n "$APPLIED" ] && [ -n "$CURRENT" ] && [ "$APPLIED" != "$CURRENT" ]; then
    N="$(git -C "$PLAT" rev-list --count "$APPLIED..$CURRENT" 2>/dev/null)"
    echo ""
    echo "  ⬆️  Платформа уехала вперёд на ${N:-?} коммит(ов) — что приехало и применить:"
    echo "      adc sync"
  fi
fi

echo ""
