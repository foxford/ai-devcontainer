#!/usr/bin/env bash
set -euo pipefail

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RESET='\033[0m'
ok()   { echo -e "${GREEN}✓${RESET} $1"; }
warn() { echo -e "${YELLOW}!${RESET} $1"; }

# Переименовывает сообщества графа из "Community N" в осмысленные имена по
# доминирующему пути узлов (напр. "sdk/src", "logger/src", "i18n") и
# перегенерирует graph.html + .graphify_labels.json. Детерминированно, без LLM.
# Запускать после сборки/обновления графа (graphify update .).

if ! command -v graphify >/dev/null 2>&1; then
  warn "graphify не найден — пропускаю path-лейблинг"; exit 0
fi
# graphify-у нужен его собственный python (API to_html, networkx)
PY="$(head -1 "$(command -v graphify)" | sed 's/^#!//')"
[ -x "$PY" ] || PY="python3"

ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
OUT="$ROOT/graphify-out"
if [ ! -f "$OUT/graph.json" ]; then
  warn "нет $OUT/graph.json — сначала собери граф: graphify update ."; exit 0
fi

summary="$(GRAPHIFY_VIZ_NODE_LIMIT="${GRAPHIFY_VIZ_NODE_LIMIT:-50000}" "$PY" - "$OUT" <<'PYEOF'
import json, collections, sys, os
import networkx as nx
from graphify.export import to_html

OUT = sys.argv[1]
data = json.load(open(f"{OUT}/graph.json"))
try:
    G = nx.node_link_graph(data, edges="links")
except TypeError:
    G = nx.node_link_graph(data)

comm = collections.defaultdict(list)
for n, d in G.nodes(data=True):
    c = d.get("community")
    if c is not None:
        comm[int(c)].append(n)

def pref(path, k):
    parts = [p for p in path.split("/") if p]
    return "/".join(parts[:k])

def label_for(ids):
    paths = [G.nodes[i].get("source_file", "") for i in ids if G.nodes[i].get("source_file")]
    if not paths:
        return None
    n = len(paths)
    # 3 сегмента (packages/<pkg>/<sub>), иначе 2 — если покрывает большинство
    for k in (3, 2):
        top, c = collections.Counter(pref(p, k) for p in paths).most_common(1)[0]
        if top and c / n >= 0.55:
            if k == 3 and "." in top.split("/")[-1]:  # доминирует файл (конфиг-пакет) → к 2 сегм.
                continue
            return top.replace("packages/", "")
    # размытое сообщество — топ-2 пакета через разделитель
    tops = [t.replace("packages/", "") for t, _ in collections.Counter(pref(p, 2) for p in paths).most_common(2)]
    return " · ".join(tops)

labels = {cid: (label_for(ids) or f"Community {cid}") for cid, ids in comm.items()}
json.dump({str(k): v for k, v in labels.items()},
          open(f"{OUT}/.graphify_labels.json", "w"), ensure_ascii=False, indent=2)

limit = int(os.environ.get("GRAPHIFY_VIZ_NODE_LIMIT", "50000"))
to_html(G, dict(comm), f"{OUT}/graph.html", community_labels=labels, node_limit=limit)

named = sum(1 for v in labels.values() if not v.startswith("Community "))
print(f"{named}/{len(labels)}")
PYEOF
)"

ok "Сообщества переименованы по путям: ${summary} с осмысленным именем. graph.html обновлён."
