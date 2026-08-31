# `packages/hermes-process`

Весь код Hermes-пайплайна в одном пакете: Node state machine, которая ведёт
командный pipeline через Hermes Kanban, и плагин дашборда `profile-skills`.

Обычный пакет монорепы. Билдится через `tsup` (как остальные пакеты),
запускается через `node`. Никакого Bun, никаких отдельных runtime-ов.

## Что внутри

```
src/
├── state-machine/         ← Node state machine
│   ├── pipeline.ts        ← граф процесса. ВСЕ изменения процесса начинаются здесь.
│   ├── machine.ts         ← engine: ловит события из kanban.db, создаёт следующие задачи
│   ├── kanban.ts          ← обёртка над `hermes kanban` CLI (через child_process)
│   ├── db.ts              ← better-sqlite3 — чтение kanban.db, запись своего state.db
│   ├── server.ts          ← node:http /validate для шелл-хуков
│   ├── index.ts           ← entry point: server + event loop + защита от двойного запуска
│   ├── log.ts
│   └── schemas/index.ts   ← Zod-схемы handoff metadata per node
└── plugins/profile-skills/  ← плагин дашборда (scoping скиллов по профилю)
    ├── ui/index.tsx       ← IIFE-бандл для дашборда (React из window.__HERMES_PLUGIN_SDK__)
    ├── manifest.json      ← статика (копируется в build на сборке)
    └── plugin_api.py      ← FastAPI-роутер плагина
```

Один `tsup` build кладёт оба таргета в единый самодостаточный `build/`:

```
build/
├── state-machine/index.js              ← Node-бандл (external better-sqlite3 — native-модуль)
└── plugins/profile-skills/dashboard/
    ├── manifest.json
    ├── plugin_api.py
    └── dist/index.js                   ← browser IIFE (имя dist — контракт manifest.entry)
```

`.hermes/bootstrap.sh` запускает `node build/state-machine/index.js` и симлинкует
`build/plugins/profile-skills/` в `~/.hermes/plugins/`.

## Скрипты

```bash
# install + build (так и делает .hermes/bootstrap.sh)
pnpm --filter @foxford/hermes-process build

# hot-reload во время разработки (TS читается прямо через tsx)
pnpm --filter @foxford/hermes-process dev

# type check без emit
pnpm --filter @foxford/hermes-process lint:type-check

# clean build artifacts
pnpm --filter @foxford/hermes-process clean
```

## Как работает kickoff

В Hermes Dashboard разработчик создаёт задачу с `assignee = team-lead`.
Опционально в первой строке body указывает шаблон:

```
template: feature       ← по умолчанию, можно не писать
template: doc-only      ← только документация
template: bugfix        ← лёгкий путь dev→review→qa→done
```

State machine видит `task_events.kind=created`, проверяет что у задачи
нет `parents` и нет нашего METADATA-блока → это user-request →
запускает pipeline.

**Никаких CLI, никаких `bun run …`, никаких slash-команд.**

## Где менять что

| Хочу поменять                                  | Файл                       |
| ---------------------------------------------- | -------------------------- |
| Добавить новый узел в граф                     | `src/state-machine/pipeline.ts` (NODES)  |
| Поменять шаблон (feature/doc-only/bugfix)      | `src/state-machine/pipeline.ts` (TEMPLATES) |
| Изменить контракт handoff metadata             | `src/state-machine/schemas/index.ts` |
| Лимиты циклов                                  | `src/state-machine/pipeline.ts` (MAX_*_CYCLES) |
| Префикс id задачи (DEV-N)                      | `src/state-machine/pipeline.ts` (TASK_PREFIX) |

## Перезапуск state machine после правок

```bash
# 1. Пересобрать
pnpm --filter @foxford/hermes-process build

# 2. Перезапустить gateway, он же поднимет обновлённый бандл
hermes gateway restart
```

## Env vars

| Переменная               | Что делает                                             | Дефолт            |
| ------------------------ | ------------------------------------------------------ | ----------------- |
| `HERMES_BOARD`           | имя kanban board                                       | `default`         |
| `HERMES_PROJECT_ROOT`    | корень репо для git worktree операций                  | `process.cwd()`   |
| `HERMES_PROJECT_SLUG`    | используется в пути `~/.hermes/state-machine/<slug>.db`| `default-project` |
| `HERMES_SM_PORT`         | порт HTTP сервера валидации                            | `43210`           |
| `HERMES_SM_POLL_MS`      | интервал чтения task_events                            | `500`             |
| `HERMES_SM_LOG_LEVEL`    | `debug` / `info` / `warn` / `error`                    | `info`            |

`bootstrap.sh` прописывает их в gateway hook handler.py через templated вставку.

## Связь с другими частями

- **`.hermes/hermes-hooks/state-machine-launcher/`** — gateway hook,
  стартует `node build/state-machine/index.js` при старте gateway.
- **`.hermes/hermes-hooks/validation/validate-handoff.sh`** — шелл-хук,
  POST-ит payload на наш HTTP `/validate`.
- **`.hermes/profiles/<role>/`** — определения ролей (identity).
- **`.agents/skills/<skill>/`** — переиспользуемые тех-skills (не роли), единый источник для всех агентов.
- **`.hermes/bootstrap.sh`** — устанавливает всё в `~/.hermes/`,
  собирает этот пакет, регистрирует профайлы и хуки.
