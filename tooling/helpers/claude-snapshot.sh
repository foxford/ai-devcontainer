#!/usr/bin/env bash
# claude-snapshot — страховка переписки Claude Code внутри проекта.
#
#   claude-snapshot            — снять снапшот сейчас
#   claude-snapshot daily      — снять, если последнему больше суток (тихо, для motd)
#   claude-snapshot list       — что уже лежит в архиве
#   claude-snapshot restore <файл> — распаковать архив обратно в projects/
#   claude-snapshot retention  — отключить автоуборку транскриптов
#
# ЗАЧЕМ. Claude Code хранит диалоги в ~/.claude/projects/<путь>/<сессия>.jsonl и
# сам подчищает их по retention (cleanupPeriodDays, дефолт 30 дней). Плюс
# ~/.claude.json умеет обнуляться, унося список сессий. В одном из проектов так
# пропала вся переписка старше двух суток — восстанавливать оказалось нечего.
#
# ГДЕ ЛЕЖИТ. Архивы — в ~/.claude/transcript-backups/, то есть внутри
# per-project персиста (bind на хост), поэтому переживают rebuild контейнера и
# не попадают под уборку, которая трогает только projects/.

set -euo pipefail

CLAUDE_DIR="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
SRC="$CLAUDE_DIR/projects"
DEST="$CLAUDE_DIR/transcript-backups"
KEEP=30                     # сколько архивов держим
STALE_HOURS=24              # порог для режима daily

C_GREEN='\033[0;32m'; C_YELLOW='\033[0;33m'; C_RESET='\033[0m'
log()  { echo -e "${C_GREEN}==>${C_RESET} $*"; }
warn() { echo -e "${C_YELLOW}!! ${C_RESET}$*" >&2; }

# Отключить автоуборку транскриптов. Правим ТОЛЬКО если ключа нет — если
# человек выставил своё значение осознанно, не спорим.
cmd_retention() {
  local settings="$CLAUDE_DIR/settings.json"
  command -v python3 >/dev/null 2>&1 || { warn "нет python3 — retention не трогаю"; return 0; }
  python3 - "$settings" <<'PY'
import json, os, sys
p = sys.argv[1]
try:
    d = json.load(open(p)) if os.path.exists(p) and os.path.getsize(p) else {}
except Exception:
    print("  settings.json битый — не трогаю"); raise SystemExit(0)
if "cleanupPeriodDays" in d:
    raise SystemExit(0)
d["cleanupPeriodDays"] = 3650
os.makedirs(os.path.dirname(p), exist_ok=True)
json.dump(d, open(p, "w"), indent=2, ensure_ascii=False)
print("  cleanupPeriodDays=3650 — автоуборка транскриптов отключена")
PY
}

snapshot_needed() {
  [ -d "$SRC" ] || return 1
  # есть ли вообще что архивировать
  [ -n "$(find "$SRC" -name '*.jsonl' -print -quit 2>/dev/null)" ] || return 1
  local last
  last="$(find "$DEST" -name 'transcripts-*.tar.gz' -printf '%T@\n' 2>/dev/null | sort -n | tail -1)"
  [ -n "$last" ] || return 0
  # свежее порога — не дублируем
  [ "$(( ($(date +%s) - ${last%.*}) / 3600 ))" -ge "$STALE_HOURS" ]
}

cmd_snapshot() {
  local quiet="${1:-}"
  if [ ! -d "$SRC" ]; then
    [ "$quiet" = quiet ] || warn "нет $SRC — нечего снимать"
    return 0
  fi
  mkdir -p "$DEST"
  local stamp archive
  stamp="$(date +%Y%m%d-%H%M%S)"
  archive="$DEST/transcripts-$stamp.tar.gz"
  tar -czf "$archive" -C "$CLAUDE_DIR" projects 2>/dev/null || {
    warn "tar завершился с ошибкой"; rm -f "$archive"; return 1
  }
  # ротация: держим последние $KEEP
  find "$DEST" -name 'transcripts-*.tar.gz' -printf '%T@ %p\n' 2>/dev/null \
    | sort -rn | tail -n +$((KEEP + 1)) | cut -d' ' -f2- \
    | while IFS= read -r old; do rm -f "$old"; done
  [ "$quiet" = quiet ] || log "снапшот: $archive ($(du -h "$archive" | cut -f1))"
}

cmd_daily() {
  snapshot_needed || exit 0
  cmd_snapshot quiet
}

cmd_list() {
  [ -d "$DEST" ] || { log "снапшотов ещё нет"; return 0; }
  find "$DEST" -name 'transcripts-*.tar.gz' -printf '%TY-%Tm-%Td %TH:%TM  %10s  %p\n' | sort
}

cmd_restore() {
  local archive="${1:?Использование: claude-snapshot restore <архив>}"
  [ -f "$archive" ] || { warn "нет файла $archive"; exit 1; }
  # Не перезаписываем вслепую: распаковываем рядом, дальше человек сам решает,
  # что вернуть — иначе восстановление старого затрёт свежие сессии.
  local out="$CLAUDE_DIR/restored-$(date +%Y%m%d-%H%M%S)"
  mkdir -p "$out"
  tar -xzf "$archive" -C "$out"
  log "распаковано в $out"
  echo "    Сравни с $SRC и перенеси нужные .jsonl руками —"
  echo "    прямая перезапись затёрла бы текущие сессии."
}

case "${1:-snapshot}" in
  snapshot|"")  cmd_snapshot;;
  daily)        cmd_daily;;
  list)         cmd_list;;
  restore)      shift; cmd_restore "$@";;
  retention)    cmd_retention;;
  help|--help|-h) sed -n '3,9p' "$0" | sed 's/^# \{0,1\}//';;
  *) warn "неизвестная команда: $1 (см. claude-snapshot help)"; exit 1;;
esac
