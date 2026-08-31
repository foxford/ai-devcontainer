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
  exit 1
}

[[ $# -lt 1 || $# -gt 2 ]] && usage

PKG="$1"
VER="${2:-}"

# Version-independent analyzer: reads declared ranges straight from the pnpm
# store (node_modules/.pnpm) + workspace manifests. Does NOT parse `pnpm why`
# output, whose JSON shape changes between pnpm majors — works the same on v7..v11.
ANALYZER="$(mktemp 2>/dev/null || echo "/tmp/pnpm-patch-dep-analyzer.$$.cjs")"
trap 'rm -f "$ANALYZER"' EXIT
cat > "$ANALYZER" <<'PATCH_DEP_ANALYZER_EOF'
'use strict';
const fs = require('fs');
const path = require('path');

const PKG = process.argv[2];
const TVER = process.argv[3];
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

PKG_JSON="package.json"

if [[ ! -f "$PKG_JSON" ]]; then
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

# Backup
cp "$PKG_JSON" "${PKG_JSON}.bak"
restore_backup() { mv -f "${PKG_JSON}.bak" "$PKG_JSON" 2>/dev/null; rm -f "$ANALYZER"; }
# ERR ловит только необработанные сбои (set -e). Явные `if ! CMD; then exit 1;
# fi` ниже (шаги 2 и 4, чтобы напечатать вывод pnpm перед выходом) trap НЕ
# триггерят — там restore_backup вызывается вручную, тем же кодом.
trap 'restore_backup; err "Failed — package.json restored from backup"' ERR

# Step 1: Add override
OVERRIDE_KEY="${PKG}@>=${MAJOR}.0.0 <${VER}"
log "Adding override: ${DIM}${OVERRIDE_KEY} → ${VER}${RESET}"
node -e "
const fs = require('fs');
const pkg = JSON.parse(fs.readFileSync('$PKG_JSON', 'utf8'));
if (!pkg.pnpm) pkg.pnpm = {};
if (!pkg.pnpm.overrides) pkg.pnpm.overrides = {};
pkg.pnpm.overrides['$OVERRIDE_KEY'] = '$VER';
fs.writeFileSync('$PKG_JSON', JSON.stringify(pkg, null, 2) + '\n');
"

# Step 2: pnpm i with override
log "Installing with override..."
if ! OUTPUT=$(pnpm i --force 2>&1); then
  echo -e "${DIM}${OUTPUT}${RESET}"
  restore_backup
  err "pnpm install failed — package.json restored from backup"
  exit 1
fi

# Step 3: Remove override
log "Removing override..."
node -e "
const fs = require('fs');
const pkg = JSON.parse(fs.readFileSync('$PKG_JSON', 'utf8'));
delete pkg.pnpm.overrides['$OVERRIDE_KEY'];
if (Object.keys(pkg.pnpm.overrides).length === 0) delete pkg.pnpm.overrides;
if (Object.keys(pkg.pnpm).length === 0) delete pkg.pnpm;
fs.writeFileSync('$PKG_JSON', JSON.stringify(pkg, null, 2) + '\n');
"

# Step 4: pnpm i without override
log "Reinstalling without override..."
if ! OUTPUT=$(pnpm i 2>&1); then
  echo -e "${DIM}${OUTPUT}${RESET}"
  restore_backup
  err "pnpm install failed — package.json restored from backup"
  exit 1
fi

# Cleanup backup
rm -f "${PKG_JSON}.bak"

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
log "Adding permanent override to package.json"
node -e "
const fs = require('fs');
const pkg = JSON.parse(fs.readFileSync('$PKG_JSON', 'utf8'));
if (!pkg.pnpm) pkg.pnpm = {};
if (!pkg.pnpm.overrides) pkg.pnpm.overrides = {};
pkg.pnpm.overrides['$OVERRIDE_KEY'] = '$VER';
fs.writeFileSync('$PKG_JSON', JSON.stringify(pkg, null, 2) + '\n');
"

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
warn "Permanent override added to package.json: ${DIM}${OVERRIDE_KEY} → ${VER}${RESET}"
warn "Remove it when upstream updates their dependency"

# Show exactly which dependency must move so the override becomes unnecessary
analyze
