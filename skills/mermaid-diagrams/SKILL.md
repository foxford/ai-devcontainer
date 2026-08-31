---
name: "mermaid-diagrams"
description: "Диаграммы и схемы на Mermaid — когда просят нарисовать, визуализировать или показать схему: flowchart, sequence, ER, state machine, архитектуру, Gantt в markdown. Срабатывай на «нарисуй», «визуализируй», «схема», «диаграмма» даже без слова mermaid."
slug: "mermaid-diagrams"
metadata:
---

# Mermaid Diagrams

Generate clean, well-structured Mermaid diagrams that render correctly in GitHub, GitLab, VSCode, and other markdown renderers.

## Choosing the right diagram type

Pick the type that best communicates the idea. Here's a quick guide:

| User wants to show | Diagram type | When to use |
|---|---|---|
| Step-by-step process | `flowchart` | Decisions, branching logic, workflows |
| Who talks to whom, in order | `sequenceDiagram` | API calls, protocols, request/response flows |
| System states and transitions | `stateDiagram-v2` | Lifecycle, status machines (active/paused/banned) |
| System components and relationships | `C4Context` or `flowchart` | Architecture overview, service dependencies |
| Database schema | `erDiagram` | Tables, relationships, data models |
| Timeline / schedule | `gantt` | Project planning, phases |
| Class hierarchy | `classDiagram` | OOP design, interfaces |
| Git branching | `gitgraph` | Release strategy, branching model |

If unsure, default to `flowchart` for processes and `sequenceDiagram` for interactions.

## Syntax guidelines

### Flowchart

```mermaid
flowchart TD
    A[Start] --> B{Decision?}
    B -->|Yes| C[Action 1]
    B -->|No| D[Action 2]
    C --> E[End]
    D --> E
```

Key points:
- `TD` = top-down, `LR` = left-right. Use LR for wide diagrams, TD for tall ones.
- Node shapes: `[rectangle]`, `{diamond/decision}`, `([stadium])`, `((circle))`, `[/parallelogram/]`
- Subgraphs group related nodes: `subgraph Title ... end`
- Keep labels short — long text breaks layout
- `\n` does NOT work in node labels — keep labels on one line
- Avoid special characters inside `[]` labels: `:`, `/`, `()` can break parsing. Keep labels simple: `A[My Node]`
- `<-->` bidirectional arrows don't work in all renderers — use `-->` only
- `---` invisible links can cause issues — use `~~~` for spacing
- `-.-` for dotted lines
- Subgraph labels with parentheses break: `subgraph Name[Label Text]` without `()`
- Cyrillic works but test in target renderer

### Sequence Diagram

```mermaid
sequenceDiagram
    participant A as Admin
    participant S as Server
    participant B as Box

    A->>S: deploy.sh
    activate S
    S-->>A: credentials + WG config
    deactivate S

    B->>S: POST /bootstrap
    activate S
    S-->>B: {uuid, pubkey, wg_config}
    deactivate S
    B->>S: VLESS connect
    Note over B,S: VPN tunnel established
```

Key points:
- `participant X as Label` for readable names
- `->>` solid arrow (request), `-->>` dashed (response)
- `activate`/`deactivate` for lifelines
- `Note over X,Y: text` for annotations
- `loop`, `alt`/`else`, `opt` for control flow
- `rect rgb(...)` for highlighting sections

### State Diagram

```mermaid
stateDiagram-v2
    [*] --> active: Add Box
    active --> paused: Pause
    paused --> active: Resume
    active --> banned: Ban
    paused --> banned: Ban
    banned --> active: Unban + auto re-register
```

### Entity Relationship

```mermaid
erDiagram
    BOXES ||--o{ CONNECTIONS : "assigned to"
    BOXES {
        int id PK
        string name
        string fingerprint
        string status
    }
    CONNECTIONS {
        int id PK
        string name
        string sub_url
    }
```

### C4 Context (architecture)

```mermaid
C4Context
    title System Context
    Person(admin, "Admin", "Manages boxes")
    System(server, "VPN Server", "3x-ui + WG + Panel")
    System(box, "Box", "OpenWrt + Passwall2")
    Rel(admin, server, "WireGuard + HTTPS")
    Rel(box, server, "VLESS Reality + WG")
```

## Style and readability

### Keep it clean
- 15-20 nodes max per diagram. Split complex systems into multiple diagrams.
- Use subgraphs to group related components.
- Label edges — unlabeled arrows are ambiguous.
- Consistent naming: either all lowercase-with-dashes or all CamelCase, not mixed.

### Make it fit the context
- For README/docs: simpler diagrams, fewer details, focus on the "what"
- For SPEC: detailed diagrams with all interactions, focus on the "how"
- For debugging: sequence diagrams showing exact request/response flow

### Color and styling (when needed)

```mermaid
flowchart LR
    A[Public] --> B[Server]
    B --> C[Internal]

    style A fill:#059669,color:#fff
    style B fill:#2563eb,color:#fff
    style C fill:#dc2626,color:#fff
```

Use sparingly — colors should highlight important distinctions (public vs private, success vs error), not decorate.

## Common patterns

### Request/response with error handling

```mermaid
sequenceDiagram
    Client->>Server: POST /api/resource
    alt success
        Server-->>Client: 200 {data}
    else not found
        Server-->>Client: 404 {error}
    else unauthorized
        Server-->>Client: 401 {error}
    end
```

### Deployment pipeline

```mermaid
flowchart LR
    subgraph Local
        A[Code] --> B[Build]
    end
    subgraph Server
        C[Deploy] --> D[Configure] --> E[Run]
    end
    B -->|scp| C
```

### State machine with actions

```mermaid
stateDiagram-v2
    state active {
        [*] --> running
        running --> syncing: heartbeat
        syncing --> running: config applied
    }
    active --> paused: admin pause
    paused --> active: admin resume
```

## Outputting diagrams

Place diagrams in fenced code blocks with the `mermaid` language tag:

````
```mermaid
flowchart TD
    A --> B
```
````

When adding to existing documents:
- Place diagrams near the text they illustrate
- Add a brief description before the diagram explaining what it shows
- Don't replace text explanations with diagrams — use both

## Validation checklist

Before outputting, mentally verify:
- All node IDs are unique
- All referenced nodes exist (no broken arrows)
- No unescaped special characters in labels (quotes, brackets)
- Diagram renders at a reasonable size (not too wide/tall)
- Labels are concise (< 40 chars)
