# `.hermes/` — team pipeline configuration

Этот каталог — **статика для Hermes-сетапа**: хуки, описания ролей,
bootstrap. Кода тут нет — он живёт в `packages/hermes-process/` как
обычный пакет монорепы.

## TL;DR — как запустить с нуля

```bash
# 1. Поставить prerequisites (один раз на машину)
#    Node.js >= 20, jq, yq; pnpm подтянется через corepack.
#    Hermes Agent — см. https://hermes-agent.nousresearch.com/

# 2. Из корня репо
./.hermes/bootstrap.sh

# 3. Открыть Dashboard — он сам поднимет gateway, а gateway через hook
#    стартует state machine. Дальше создать задачу:
hermes dashboard
```

В Dashboard создаёшь задачу с:

- **Assignee:** `team-lead`
- **Title:** короткое имя фичи
- **Body:** опционально первой строкой `template: doc-only` или `template: bugfix`, дальше описание/AC

State machine ловит её через `task_events.kind=created`, видит что parents
пусты и нет нашего METADATA-блока → понимает что это user-request → запускает
pipeline. На задачу автоматически добавится комментарий с подтверждением.

## Что где

```
.hermes/                           ← статика (этот каталог)
├── README.md                      ← ты здесь
├── bootstrap.sh                   ← idempotent installer (см. ниже)
├── profile-skills.yaml            ← allow-list скиллов по ролям (источник правды для сида)
├── hermes-hooks/
│   ├── state-machine-launcher/    ← gateway hook, стартует node-бандл
│   │   ├── HOOK.yaml
│   │   └── handler.py             ← templated с PROJECT_ROOT при bootstrap
│   └── validation/
│       └── validate-handoff.sh    ← pre_tool_call → curl → HTTP /validate
├── profiles/                      ← роли (identity профайлов)
│   ├── team-lead/
│   │   └── SOUL.md                ← system prompt / personality (Hermes-native filename)
│   ├── tech-lead/SOUL.md
│   ├── developer/SOUL.md
│   ├── qa/SOUL.md
│   ├── documenter/SOUL.md
│   └── security-auditor/SOUL.md
└── skills/                        ← переиспользуемые тех-skills (НЕ роли!)
    ├── README.md                  ← как сюда копировать company skills
    └── (твои тех-skills после копирования)

packages/hermes-process/           ← весь код Hermes в одном пакете
├── README.md
├── package.json                   ← scripts: dev/build/typecheck
├── tsconfig.json
├── src/
│   ├── state-machine/             ← Node state machine
│   │   ├── pipeline.ts            ← граф процесса (source of truth)
│   │   ├── machine.ts             ← engine
│   │   ├── kanban.ts              ← hermes kanban CLI wrapper
│   │   ├── db.ts                  ← better-sqlite3 read/write
│   │   ├── server.ts              ← HTTP /validate
│   │   ├── index.ts               ← entry point
│   │   ├── log.ts
│   │   └── schemas/index.ts       ← Zod-схемы handoff metadata
│   └── plugins/profile-skills/    ← плагин дашборда (scoping скиллов по профилю)
│       ├── ui/index.tsx           ← IIFE-бандл для дашборда
│       ├── manifest.json          ← статика (копируется в build на сборке)
│       └── plugin_api.py          ← FastAPI-роутер плагина
└── build/
    ├── state-machine/index.js     ← Node-бандл (запускается через node)
    └── plugins/profile-skills/dashboard/
        ├── manifest.json
        ├── plugin_api.py
        └── dist/index.js          ← browser IIFE
```

## Что делает bootstrap.sh

1. Проверяет prerequisites (`node`, `pnpm` через corepack, `hermes`, `jq`, `yq`)
2. `pnpm install` + `pnpm --filter @foxford/hermes-process build` → `build/state-machine/index.js` + `build/plugins/profile-skills/`
3. Создаёт kanban board под проект
4. Для каждой роли: `hermes profile create <role>` (если ещё нет), копирует `SOUL.md` в `~/.hermes/profiles/<role>/SOUL.md`, и **сидит скоупинг скиллов** из allow-list [`profile-skills.yaml`](profile-skills.yaml). Семантика: перечисленное в allow-list = включено, всё прочее из вселенной скиллов на машине bootstrap пишет в `skills.disabled` профиля. В Hermes нет «добавить скилл профилю» — клон видит все скиллы из `external_dirs`, поэтому сужаем через `disabled`. **Сид одноразовый**: маркер `skills.foxford_seed` помечает, что базлайн разложен; при повторных запусках bootstrap профиль НЕ трогает — дальше скоупинг живёт в дашборд-плагине `profile-skills` (или `hermes skills config`). Чтобы поменять базлайн для команды — правь `profile-skills.yaml`
5. Регистрирует `.agents/skills/` в `~/.hermes/config.yaml` (`skills.external_dirs`)
6. Копирует gateway hook в `~/.hermes/hooks/state-machine-launcher-<project>/` с подстановкой `PROJECT_ROOT`
7. Прописывает шелл-хук валидации в `~/.hermes/config.yaml` (`hooks.pre_tool_call`)
8. Симлинкует собранный дашборд-плагин `build/plugins/profile-skills/` в `~/.hermes/plugins/` и темы из `dashboard-themes/` в `~/.hermes/dashboard-themes/`

Скрипт идемпотентный. Перезапуск bootstrap'а после правок `.hermes/profiles/*/SOUL.md` обновит system prompt всех профилей.

## Templates

| Template     | Слот в графе                                                                          |
| ------------ | ------------------------------------------------------------------------------------- |
| `feature`    | plan (tech) → decompose (team) → для каждой impl: dev → review → qa → (sec?) → (docs?) → final-verify |
| `doc-only`   | documenter → light tech-lead verify → done                                            |
| `bugfix`     | dev → review → qa → done                                                              |

Security и docs внутри `feature` — флаги в handoff metadata tech-lead-а на
узле `route-post-qa` (`needs_security`, `doc_impact`). По умолчанию оба false.

## Где смотреть статус

```bash
hermes dashboard                                            # web GUI
hermes kanban watch                                         # CLI live-стрим
hermes kanban list                                          # snapshot
tail -f ~/.hermes/logs/state-machine-<project-slug>.log     # лог state machine
```

## Перезапуск после изменения процесса

```bash
# 1. Пересобрать
pnpm --filter @foxford/hermes-process build

# 2. Перезапустить gateway — он перезапустит state machine
hermes gateway restart
```

## Известные ограничения

Несколько вещей зависят от точной схемы/CLI реального Hermes-инстанса.
При первом прогоне может понадобиться минорная подгонка:

- **Model для профайлов.** Hermes не позволяет указать модель напрямую через файл в каталоге профайла; задаётся через `hermes profile model <role> <model-id>` либо через UI (страница Profiles). Bootstrap её не выставляет — наследуется глобальная.

- **`db.ts:fetchTask`** делает SQL по `tasks.workspace_spec`. Имя колонки в реальной схеме Hermes-овской kanban.db может быть другим. Если на первом запуске будет ошибка `no such column` — посмотри `sqlite3 ~/.hermes/kanban.db .schema tasks` и поправь.

- **CLI флаги в `kanban.ts`** (`--workspace`, `--idempotency-key`, `--skill`, `--metadata`) — основаны на docs. Если `hermes kanban create` падает с unknown flag — посмотри `hermes kanban create --help` и поправь.

## Что-то сломалось

```bash
# State machine не запустилась
cat ~/.hermes/logs/state-machine-<project-slug>.log

# HTTP сервер не отвечает
curl http://127.0.0.1:43210/health     # должно вернуть "ok"

# Полный ресет state.db (kanban не трогает)
rm -rf ~/.hermes/state-machine/<project-slug>.db
hermes gateway restart

# Пересобрать бандл если что-то поменялось
pnpm --filter @foxford/hermes-process build
```
