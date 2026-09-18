#!/usr/bin/env bash
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
DIM='\033[2m'
RESET='\033[0m'

log()  { echo -e "${CYAN}→${RESET} $1"; }
ok()   { echo -e "${GREEN}✓${RESET} $1"; }
warn() { echo -e "${YELLOW}!${RESET} $1"; }
err()  { echo -e "${RED}✗${RESET} $1"; }

usage() {
  echo -e "Usage: ${CYAN}pnpm-patch-dep${RESET} <package> [version|major]"
  echo ""
  echo "  Update a transitive dependency in pnpm-lock.yaml via override dance."
  echo "  Targets only the matching major version to avoid cross-major breakage."
  echo ""
  echo -e "  ${DIM}pnpm-patch-dep nx${RESET}               # latest version from registry"
  echo -e "  ${DIM}pnpm-patch-dep picomatch 4.0.4${RESET}    # exact version"
  echo -e "  ${DIM}pnpm-patch-dep picomatch 4${RESET}        # latest 4.x from registry"
  echo -e "  ${DIM}pnpm-patch-dep braces 3.0.3${RESET}"
  echo ""
  echo -e "  ${CYAN}pnpm-patch-dep doctor${RESET}         # ревизия постоянных override'ов:"
  echo -e "  ${DIM}                              какие уже можно снять, кто держит остальные,${RESET}"
  echo -e "  ${DIM}                              вышла ли у держателя версия без ограничения.${RESET}"
  echo -e "  ${DIM}                              Код возврата 1, если есть что сделать.${RESET}"
  exit 1
}

DOCTOR=0
if [[ "${1:-}" == "doctor" ]]; then
  [[ $# -eq 1 ]] || usage
  DOCTOR=1
fi

[[ $# -lt 1 || $# -gt 2 ]] && usage

PKG="$1"
VER="${2:-}"

# Version-independent analyzer: reads declared ranges straight from the pnpm
# store (node_modules/.pnpm) + workspace manifests. Does NOT parse `pnpm why`
# output, whose JSON shape changes between pnpm majors — works the same on v7..v11.
ANALYZER="$(mktemp 2>/dev/null || echo "/tmp/pnpm-patch-dep-analyzer.$$.cjs")"
NPM_ERR="$(mktemp 2>/dev/null || echo "/tmp/pnpm-patch-dep-npm.$$.log")"
trap 'rm -f "$ANALYZER" "$NPM_ERR"' EXIT
cat > "$ANALYZER" <<'PATCH_DEP_ANALYZER_EOF'
'use strict';
const fs = require('fs');
const path = require('path');

// Один модуль на всю чистую логику скрипта. Режимы, кроме --report, обслуживают
// `doctor`: он живёт в bash, а сравнение версий с диапазоном спрашивает здесь —
// вторая реализация семвера в репозитории разъехалась бы с первой в первый же
// месяц, и тогда «можно снять» и «ещё нужен» начали бы расходиться молча.
//   --report     PKG VER    (по умолчанию) человекочитаемый разбор капперов
//   --json       PKG VER    те же капперы машинно
//   --satisfies  VER RANGE  только код возврата
//   --dep-range  PKG        манифест со stdin → "<версия>\t<диапазон|->"
//   --overrides  FILE…      объявленные override'ы → "<файл>\t<ключ>\t<версия>"
const argv = process.argv.slice(2);
const MODE = (argv[0] || '').startsWith('--') ? argv.shift() : '--report';
const PKG = argv[0];
const TVER = argv[1];
const C='\x1b[0;36m', G='\x1b[0;32m', Y='\x1b[1;33m', D='\x1b[2m', R='\x1b[0m';

/* mini semver (^ ~ >= <= > < = ranges, || / space sets) */
const parse = v => { const m=String(v).split('+')[0].split('-')[0].split('.');
  return [parseInt(m[0]||0,10),parseInt(m[1]||0,10),parseInt(m[2]||0,10)]; };
const cmp = (a,b) => a[0]-b[0] || a[1]-b[1] || a[2]-b[2];
function comparatorOk(ver, c){
  c=c.trim(); if(!c||c==='*'||c==='x'||c==='X') return true;
  const m=c.match(/^(>=|<=|>|<|=|\^|~)?\s*(.*)$/); const op=(m&&m[1])||''; const rest=(m&&m[2])||c;
  const p=rest.split('.'); const M=parseInt(p[0]||0,10);
  const mi=p[1]===undefined?null:parseInt(p[1],10); const pa=p[2]===undefined?null:parseInt(p[2],10);
  const base=[M,mi||0,pa||0]; const v=parse(ver);
  if(op==='^'){ let up; if(M>0)up=[M+1,0,0]; else if((mi||0)>0)up=[0,mi+1,0]; else up=[0,0,(pa||0)+1];
    return cmp(v,base)>=0 && cmp(v,up)<0; }
  if(op==='~'){ const up=mi===null?[M+1,0,0]:[M,mi+1,0]; return cmp(v,base)>=0 && cmp(v,up)<0; }
  if(op==='>=')return cmp(v,base)>=0; if(op==='<=')return cmp(v,base)<=0;
  if(op==='>') return cmp(v,base)>0;  if(op==='<') return cmp(v,base)<0;
  if(mi===null)return v[0]===M; if(pa===null)return v[0]===M&&v[1]===mi; return cmp(v,base)===0;
}
const satisfies = (ver,range) => !range ? null :
  range.split('||').some(or => or.trim().split(/\s+/).every(c => comparatorOk(ver,c)));

if (MODE === '--satisfies') process.exit(satisfies(PKG, TVER) === true ? 0 : 1);

if (MODE === '--dep-range') {
  let raw = ''; try { raw = fs.readFileSync(0, 'utf8'); } catch {}
  let m; try { m = JSON.parse(raw); } catch { process.exit(1); }
  if (Array.isArray(m)) m = m[m.length - 1];
  if (!m || typeof m !== 'object') process.exit(1);
  const f = ['dependencies','peerDependencies','optionalDependencies']
    .find(k => m[k] && m[k][PKG]);
  console.log(`${m.version || '?'}\t${f ? m[f][PKG] : '-'}`);
  process.exit(0);
}

if (MODE === '--overrides') {
  const unq = s => s.trim().replace(/^['"]|['"]$/g, '');
  for (const file of argv) {
    let text; try { text = fs.readFileSync(file, 'utf8'); } catch { continue; }
    if (file.endsWith('.json')) {
      let ov; try { ov = (JSON.parse(text).pnpm || {}).overrides; } catch { continue; }
      for (const [k, v] of Object.entries(ov || {}))
        if (typeof v === 'string') console.log(`${file}\t${k}\t${v}`);
      continue;
    }
    // YAML: блок `overrides:` первого уровня, построчно. Структура блока
    // плоская, а парсер тянуть некуда — зависимостей у скрипта нет.
    let inBlock = false;
    for (const line of text.split('\n')) {
      if (/^overrides:\s*$/.test(line)) { inBlock = true; continue; }
      if (!inBlock) continue;
      if (/^\S/.test(line)) break;                      // пошёл следующий ключ
      if (!line.trim() || line.trim().startsWith('#')) continue;
      const m = line.trim().match(/^(.+?):\s*(.+)$/);
      if (m) console.log(`${file}\t${unq(m[1])}\t${unq(m[2])}`);
    }
  }
  process.exit(0);
}

// Пакет на СОСЕДНЕМ мажоре к делу не относится: у pnpm он получает свою копию,
// а override переписывает только окно `>=MAJOR.0.0 <TVER`. Без этого отсева
// в «кто держит nanoid ниже 5.1.16» попадал postcss со своим nanoid@^3 — шум,
// из-за которого снимать override страшно, хотя причина давно ушла.
const majorsOf = (range) => String(range).split('||').flatMap(o => o.trim().split(/\s+/))
  .map(c => c.replace(/^(>=|<=|>|<|=|\^|~)\s*/, '').trim())
  .filter(c => /^\d/.test(c))
  .map(c => parseInt(c.split('.')[0], 10))
  .filter(n => Number.isFinite(n));
const touchesMajor = (range, major) => {
  const m = majorsOf(range);
  if (!m.length) return true;                    // `*`, `x`, workspace: — судить не беремся
  return major >= Math.min(...m) && major <= Math.max(...m);
};

// Installed 3rd-party deps constrain only via dependencies/peer/optional.
// (devDependencies of a dependency are metadata pnpm never installs.)
const rangeOf = (pj) => {
  for (const f of ['dependencies','peerDependencies','optionalDependencies'])
    if (pj[f] && pj[f][PKG]) return { range: pj[f][PKG], field: f };
  return null;
};

/* scan the pnpm store for packages whose range excludes the target */
const cappers = new Map(); // name -> { ranges:Set, fields:Set }
const PNPM = path.join('node_modules', '.pnpm');
let entries = []; try { entries = fs.readdirSync(PNPM); } catch {}
for (const dir of entries) {
  const m = dir.match(/^(.+?)@(\d[^_(]*)/);   // name@version, strips _peer / (peer)
  if (!m) continue;
  const name = m[1].replace(/\+/g, '/');       // @scope+pkg -> @scope/pkg
  if (name === PKG) continue;
  let pj;
  try { pj = JSON.parse(fs.readFileSync(path.join(PNPM, dir, 'node_modules', name, 'package.json'),'utf8')); }
  catch { continue; }
  const r = rangeOf(pj);
  if (!r) continue;
  if (satisfies(TVER, r.range) === true) continue;   // allows target — not a capper
  if (!touchesMajor(r.range, parse(TVER)[0])) continue;  // другой мажор — не наше окно
  if (!cappers.has(name)) cappers.set(name, { ranges:new Set(), fields:new Set() });
  cappers.get(name).ranges.add(r.range);
  cappers.get(name).fields.add(r.field);
}

/* which workspace manifests declare each capper (so you know where to bump) */
const SKIP = new Set(['node_modules','.git','dist','build','coverage','.next','.turbo','.nx','.cache','out','tmp']);
const manifests = [];
(function walk(dir, depth){
  if (depth > 4) return;
  let items; try { items = fs.readdirSync(dir, { withFileTypes:true }); } catch { return; }
  for (const it of items) {
    if (it.isDirectory()) { if (!SKIP.has(it.name) && !it.name.startsWith('.')) walk(path.join(dir,it.name), depth+1); }
    else if (it.name === 'package.json') {
      try { manifests.push({ p:dir, pj:JSON.parse(fs.readFileSync(path.join(dir,it.name),'utf8')) }); } catch {}
    }
  }
})('.', 0);
const declarers = name => manifests
  .filter(mf => ['dependencies','devDependencies','optionalDependencies','peerDependencies']
    .some(f => mf.pj[f] && mf.pj[f][name]))
  .map(mf => mf.pj.name || mf.p);

if (MODE === '--json') {
  console.log(JSON.stringify({ cappers: [...cappers].map(([name, info]) => ({
    name, ranges: [...info.ranges], fields: [...info.fields], declarers: declarers(name),
  })) }));
  process.exit(0);
}

if (cappers.size === 0) process.exit(0);
const out = [];
out.push('');
out.push(`${C}↳ ${PKG} is held below ${G}${TVER}${C} by these declared ranges:${R}`);
for (const [name, info] of [...cappers].sort((a,b)=>a[0].localeCompare(b[0]))) {
  const ranges = [...info.ranges].map(r=>`"${r}"`).join(', ');
  const peer = info.fields.has('peerDependencies') && info.fields.size===1 ? ` ${D}(peer)${R}` : '';
  out.push(`    ${Y}${name}${R}  ${D}requires ${PKG} ${ranges}${R}${peer}`);
  const decl = declarers(name);
  if (decl.length) {
    const shown = decl.slice(0,4).join(', ');
    out.push(`        ${D}declared in: ${shown}${decl.length>4?` +${decl.length-4} more`:''}${R}`);
  } else {
    out.push(`        ${D}transitive — run \`pnpm why ${name}\` to see what pulls it${R}`);
  }
}
out.push('');
out.push(`    ${D}Bump the package(s) above to a release whose ${PKG} range allows ${TVER}, then drop the override.${R}`);
console.log(out.join('\n'));
PATCH_DEP_ANALYZER_EOF

analyze() {
  log "Analyzing what pins ${PKG} below ${VER}..."
  set +e
  node "$ANALYZER" "$PKG" "$VER" 2>/dev/null
  set -e
}

# ─── Куда класть override ────────────────────────────────────────────────────
# pnpm 11 перестал читать поле `pnpm` в package.json — настройки переехали в
# pnpm-workspace.yaml. Записанный «как раньше» override там молча игнорируется:
# install проходит успешно, lock не меняется, и скрипт рапортует «not found in
# lock file even with permanent override» — диагноз, уводящий в дерево
# зависимостей, хотя дело в адресе файла. Поэтому цель выбираем по мажору
# самого pnpm, а не угадываем.
detect_override_file() {
  PNPM_VERSION="$(pnpm --version 2>/dev/null | tr -d '[:space:]' || true)"
  PNPM_MAJOR="${PNPM_VERSION%%.*}"
  if [[ "$PNPM_MAJOR" =~ ^[0-9]+$ ]] && [[ "$PNPM_MAJOR" -ge 11 ]]; then
    OVERRIDE_FILE="pnpm-workspace.yaml"
  else
    OVERRIDE_FILE="package.json"
  fi
}

# имя пакета из ключа override: `smol-toml@>=1.0.0 <1.8.0` → smol-toml,
# `@foxford/cli@^1` → @foxford/cli, `lodash` → lodash
override_pkg_name() {
  local key="$1" name="${1%@*}"
  [[ -z "$name" ]] && name="$key"     # ключ вида `@scope/pkg` без диапазона
  printf '%s' "$name"
}

# ─── doctor ──────────────────────────────────────────────────────────────────
# Override ставится «на время», а живёт годами: причина забывается в тот же
# день, и снять его потом страшно — никто не помнит, что он держал. doctor
# отвечает ровно на это: что уже можно выкинуть, кто держит остальное и не
# вышла ли у держателя версия, где ограничение снято.
doctor() {
  detect_override_file
  log "pnpm ${PNPM_VERSION:-unknown} ${DIM}— overrides live in ${OVERRIDE_FILE}${RESET}"

  # Разбор идёт по установленному дереву: без node_modules «капперов не
  # осталось» означало бы не «можно снять», а «смотреть было не во что».
  if [[ ! -d node_modules/.pnpm ]]; then
    err "node_modules/.pnpm not found — run pnpm i first, doctor reads the installed tree"
    exit 1
  fi

  local rows
  rows="$(node "$ANALYZER" --overrides package.json pnpm-workspace.yaml 2>/dev/null || true)"
  if [[ -z "$rows" ]]; then
    ok "No overrides declared — nothing to check"
    exit 0
  fi

  local total=0 droppable=0 ignored=0 bumpable=0
  local file key ver pkg cappers_json cappers_tsv
  while IFS=$'\t' read -r file key ver; do
    [[ -z "${key:-}" ]] && continue
    total=$((total + 1))
    pkg="$(override_pkg_name "$key")"

    echo ""
    echo -e "  ${YELLOW}${key}${RESET} → ${GREEN}${ver}${RESET}  ${DIM}(${file})${RESET}"

    if [[ "$file" == "package.json" && "$PNPM_MAJOR" =~ ^[0-9]+$ && "$PNPM_MAJOR" -ge 11 ]]; then
      ignored=$((ignored + 1))
      warn "    pnpm ${PNPM_VERSION} doesn't read package.json overrides — this one does nothing"
    fi

    cappers_json="$(node "$ANALYZER" --json "$pkg" "$ver" 2>/dev/null || true)"
    cappers_tsv="$(printf '%s' "${cappers_json:-}" | node -e '
      let d = "";
      process.stdin.on("data", c => d += c).on("end", () => {
        let j; try { j = JSON.parse(d); } catch { process.exit(0); }
        for (const c of j.cappers || [])
          console.log([c.name, c.ranges.join(", "), c.declarers.slice(0, 3).join(", ")].join("\t"));
      });
    ' 2>/dev/null || true)"

    if [[ -z "$cappers_tsv" ]]; then
      droppable=$((droppable + 1))
      ok "    nothing in the tree requires ${pkg} below ${ver} any more — drop the override"
      continue
    fi

    echo -e "    ${DIM}still needed — held by:${RESET}"
    # Держат ли ВСЕ капперы до сих пор, или у каждого уже есть релиз без
    # ограничения — это разные новости: во втором случае override снимается
    # бампом, и ради этого doctor и зовут через полгода.
    local lift_all=1
    local name ranges decl manifest latest_line latest_ver latest_range
    while IFS=$'\t' read -r name ranges decl; do
      [[ -z "${name:-}" ]] && continue
      echo -e "      ${YELLOW}${name}${RESET} ${DIM}requires ${pkg} ${ranges}${RESET}${decl:+ ${DIM}(declared in: ${decl})${RESET}}"

      # Держатель мог давно выпустить релиз без ограничения — это и есть
      # момент, когда override пора снимать, а без реестра его не увидеть.
      manifest="$(npm view "${name}@latest" --json 2>"$NPM_ERR" </dev/null || true)"
      if [[ -z "$manifest" ]]; then
        if grep -q 'E404' "$NPM_ERR" 2>/dev/null; then
          echo -e "        ${DIM}not on the registry (local or unpublished) — check by hand${RESET}"
        else
          echo -e "        ${DIM}registry unavailable — can't tell if a newer ${name} lifts it${RESET}"
        fi
        lift_all=0
        continue
      fi
      latest_line="$(printf '%s' "$manifest" | node "$ANALYZER" --dep-range "$pkg" 2>/dev/null || true)"
      if [[ -z "$latest_line" ]]; then
        echo -e "        ${DIM}can't read ${name}@latest manifest — check by hand${RESET}"
        lift_all=0
        continue
      fi
      latest_ver="${latest_line%%$'\t'*}"
      latest_range="${latest_line#*$'\t'}"
      if [[ "$latest_range" == "-" ]]; then
        ok "        ${name}@${latest_ver} dropped ${pkg} entirely — bump ${name}, then drop the override"
      elif node "$ANALYZER" --satisfies "$ver" "$latest_range"; then
        ok "        ${name}@${latest_ver} requires \"${latest_range}\" — bump ${name}, then drop the override"
      else
        echo -e "        ${DIM}latest ${name}@${latest_ver} still requires \"${latest_range}\"${RESET}"
        lift_all=0
      fi
    done <<< "$cappers_tsv"

    if [[ "$lift_all" -eq 1 ]]; then
      bumpable=$((bumpable + 1))
      echo -e "    ${CYAN}↳${RESET} every holder has a release that lifts it — bump them, then drop this override"
    fi
  done <<< "$rows"

  echo ""
  if [[ "$droppable" -eq 0 && "$ignored" -eq 0 && "$bumpable" -eq 0 ]]; then
    ok "${total} override(s), every one still doing work"
    exit 0
  fi
  [[ "$droppable" -gt 0 ]] && warn "${droppable}/${total} override(s) can be dropped right now"
  [[ "$bumpable" -gt 0 ]] && warn "${bumpable}/${total} override(s) go away after bumping the holder"
  [[ "$ignored" -gt 0 ]] && warn "${ignored} override(s) sit in package.json, which this pnpm ignores"
  exit 1
}

if [[ "$DOCTOR" -eq 1 ]]; then
  doctor
fi

if [[ -z "$VER" ]]; then
  # No version given — resolve latest from registry
  log "Resolving latest ${PKG} from registry..."
  VER=$(npm view "${PKG}" version 2>/dev/null)
  if [[ -z "$VER" ]]; then
    err "Cannot resolve latest ${PKG}"
    exit 1
  fi
  MAJOR="${VER%%.*}"
  ok "Resolved → ${GREEN}${VER}${RESET}"
elif [[ "$VER" =~ ^[0-9]+$ ]]; then
  # Only major given — resolve latest of that major
  MAJOR="$VER"
  log "Resolving latest ${PKG}@${MAJOR}.x from registry..."
  VER=$(npm view "${PKG}@${MAJOR}" version --json 2>/dev/null | node -e "
    let d = '';
    process.stdin.on('data', c => d += c);
    process.stdin.on('end', () => {
      const v = JSON.parse(d);
      console.log(Array.isArray(v) ? v[v.length - 1] : v);
    });
  ")
  if [[ -z "$VER" ]]; then
    err "Cannot resolve latest ${PKG}@${MAJOR}.x"
    exit 1
  fi
  ok "Resolved → ${GREEN}${VER}${RESET}"
else
  MAJOR="${VER%%.*}"
fi


if [[ ! -f "package.json" ]]; then
  err "package.json not found in $(pwd)"
  exit 1
fi

if [[ ! -f "pnpm-lock.yaml" ]]; then
  err "pnpm-lock.yaml not found in $(pwd)"
  exit 1
fi

# Check current state
OLD_COUNT=$(grep -c "${PKG}@${MAJOR}\." pnpm-lock.yaml || true)
ALREADY=$(grep -c "${PKG}@${VER}" pnpm-lock.yaml || true)

log "Found ${YELLOW}${OLD_COUNT}${RESET} entries of ${PKG}@${MAJOR}.x in lock file"

if [[ "$OLD_COUNT" -eq 0 ]]; then
  warn "No ${PKG}@${MAJOR}.x in lock file, nothing to do"
  exit 0
fi

if [[ "$OLD_COUNT" -eq "$ALREADY" ]]; then
  ok "Already at ${PKG}@${VER}, nothing to do"
  exit 0
fi

detect_override_file
log "Overrides go to ${YELLOW}${OVERRIDE_FILE}${RESET} ${DIM}(pnpm ${PNPM_VERSION:-unknown})${RESET}"

# Старые overrides в package.json на pnpm 11 — мёртвый груз: человек их видит,
# а pnpm нет. Предупреждаем, но не трогаем: чужие записи не наши.
if [[ "$OVERRIDE_FILE" == "pnpm-workspace.yaml" ]] \
   && node -e "const p=require('./package.json'); process.exit(p.pnpm&&p.pnpm.overrides&&Object.keys(p.pnpm.overrides).length?0:1)" 2>/dev/null; then
  warn "package.json still has pnpm.overrides — this pnpm ignores them, move them to pnpm-workspace.yaml"
fi

# Файла может не быть вовсе (не-workspace проект на pnpm 11) — тогда мы его
# создаём, и «восстановление» означает удалить, а не вернуть содержимое.
if [[ -f "$OVERRIDE_FILE" ]]; then
  HAD_OVERRIDE_FILE=1
  RESTORE_NOTE="${OVERRIDE_FILE} restored from backup"
  cp "$OVERRIDE_FILE" "${OVERRIDE_FILE}.bak"
else
  HAD_OVERRIDE_FILE=0
  RESTORE_NOTE="${OVERRIDE_FILE} removed"
fi

restore_backup() {
  if [[ "$HAD_OVERRIDE_FILE" -eq 1 ]]; then
    mv -f "${OVERRIDE_FILE}.bak" "$OVERRIDE_FILE" 2>/dev/null || true
  else
    rm -f "$OVERRIDE_FILE"
  fi
  rm -f "$ANALYZER"
}
# ERR ловит только необработанные сбои (set -e). Явные `if ! CMD; then exit 1;
# fi` ниже (шаги 2 и 4, чтобы напечатать вывод pnpm перед выходом) trap НЕ
# триггерят — там restore_backup вызывается вручную, тем же кодом.
trap 'restore_backup; err "Failed — ${RESTORE_NOTE}"' ERR

OVERRIDE_KEY="${PKG}@>=${MAJOR}.0.0 <${VER}"

# add|del в обоих форматах. Ключ и версия идут через окружение: в ключе есть
# пробел, `<` и `>` — интерполировать такое в тело `node -e` значит однажды
# получить синтаксическую ошибку JS вместо override'а.
override_edit() {
  OV_MODE="$1" \
  OV_FILE="$OVERRIDE_FILE" \
  OV_KEY="$OVERRIDE_KEY" \
  OV_VER="$VER" \
  OV_HAD_FILE="$HAD_OVERRIDE_FILE" \
  node -e '
const fs = require("fs");
const { OV_MODE: mode, OV_FILE: file, OV_KEY: key, OV_VER: ver } = process.env;

if (file === "package.json") {
  const pkg = JSON.parse(fs.readFileSync(file, "utf8"));
  if (mode === "add") {
    if (!pkg.pnpm) pkg.pnpm = {};
    if (!pkg.pnpm.overrides) pkg.pnpm.overrides = {};
    pkg.pnpm.overrides[key] = ver;
  } else if (pkg.pnpm && pkg.pnpm.overrides) {
    delete pkg.pnpm.overrides[key];
    if (Object.keys(pkg.pnpm.overrides).length === 0) delete pkg.pnpm.overrides;
    if (Object.keys(pkg.pnpm).length === 0) delete pkg.pnpm;
  }
  fs.writeFileSync(file, JSON.stringify(pkg, null, 2) + "\n");
  process.exit(0);
}

// pnpm-workspace.yaml правим построчно: зависимостей у скрипта нет, тянуть
// YAML-парсер некуда. Это безопасно ровно потому, что строку мы порождаем
// сами и удаляем её же — остальной файл (packages, catalogs) не парсится.
const entry = "  \x27" + key + "\x27: \x27" + ver + "\x27";
const text = fs.existsSync(file) ? fs.readFileSync(file, "utf8") : "";
let lines = text.length ? text.replace(/\n+$/, "").split("\n") : [];
const isHeader = (l) => /^overrides:\s*(\{\s*\})?\s*$/.test(l);

lines = lines.filter((l) => l.trim() !== entry.trim());

if (mode === "add") {
  let i = lines.findIndex(isHeader);
  if (i === -1) {
    if (lines.length && lines[lines.length - 1].trim() !== "") lines.push("");
    lines.push("overrides:");
    i = lines.length - 1;
  } else {
    lines[i] = "overrides:";          // разворачиваем `overrides: {}`
  }
  lines.splice(i + 1, 0, entry);
} else {
  const i = lines.findIndex(isHeader);
  // заголовок без единого потомка — это `overrides: null`, на котором pnpm
  // падает; убираем вместе с последней записью
  if (i !== -1 && !/^\s+\S/.test(lines[i + 1] || "")) lines.splice(i, 1);
}

const out = lines.join("\n").replace(/\n+$/, "");
if (!out.trim() && process.env.OV_HAD_FILE !== "1") {
  if (fs.existsSync(file)) fs.unlinkSync(file);   // файл наш, пустым не оставляем
} else {
  fs.writeFileSync(file, out + "\n");
}
'
}

# Step 1: Add override
log "Adding override: ${DIM}${OVERRIDE_KEY} → ${VER}${RESET}"
override_edit add

# Step 2: pnpm i with override
log "Installing with override..."
if ! OUTPUT=$(pnpm i --force 2>&1); then
  echo -e "${DIM}${OUTPUT}${RESET}"
  restore_backup
  err "pnpm install failed — ${RESTORE_NOTE}"
  exit 1
fi

# Step 3: Remove override
log "Removing override..."
override_edit del

# Step 4: pnpm i without override
log "Reinstalling without override..."
if ! OUTPUT=$(pnpm i 2>&1); then
  echo -e "${DIM}${OUTPUT}${RESET}"
  restore_backup
  err "pnpm install failed — ${RESTORE_NOTE}"
  exit 1
fi

# Cleanup backup
rm -f "${OVERRIDE_FILE}.bak"

# Verify
NEW_COUNT=$(grep -c "${PKG}@${MAJOR}\." pnpm-lock.yaml || true)
PATCHED=$(grep -c "${PKG}@${VER}" pnpm-lock.yaml || true)
REMAINING=$((NEW_COUNT - PATCHED))

echo ""
if [[ "$PATCHED" -gt 0 && "$REMAINING" -eq 0 ]]; then
  ok "${GREEN}${PKG}@${VER}${RESET} — all ${PATCHED} entries patched"
  exit 0
fi

# Nothing held after the override was removed. For deep transitive deps this is
# expected: nothing in the tree natively requests the new version, so it reverts
# fully (PATCHED == 0). For partial reverts some entries stay stuck. In both
# cases we keep the override permanently so the version sticks.
if [[ "$PATCHED" -eq 0 ]]; then
  warn "Reverted after override removed (deep transitive) — adding permanent override"
else
  warn "${PATCHED}/${NEW_COUNT} patched, ${YELLOW}${REMAINING}${RESET} stuck — adding permanent override"
fi
log "Adding permanent override to ${OVERRIDE_FILE}"
override_edit add

log "Installing with permanent override..."
if ! OUTPUT=$(pnpm i 2>&1); then
  echo -e "${DIM}${OUTPUT}${RESET}"
  err "pnpm install failed"
  exit 1
fi

FINAL=$(grep -c "${PKG}@${VER}" pnpm-lock.yaml || true)
echo ""
if [[ "$FINAL" -eq 0 ]]; then
  err "Update failed — ${PKG}@${VER} not found in lock file even with permanent override"
  exit 1
fi
ok "${GREEN}${PKG}@${VER}${RESET} — ${FINAL} entries patched"
warn "Permanent override added to ${OVERRIDE_FILE}: ${DIM}${OVERRIDE_KEY} → ${VER}${RESET}"
warn "Remove it when upstream updates their dependency"

# Show exactly which dependency must move so the override becomes unnecessary
analyze
