# ADC: рабочая среда для AI-разработки

ADC помогает работать с AI-агентами в нескольких проектах. Вы подключаете свои репозитории, а агенты получают общие знания команды, карту проектов и инструкции для работы с каждым из них: как проверять код, готовить релизы и пользоваться нужными инструментами.

Все проекты доступны в одной изолированной среде. Через Orca можно запускать агентов параллельно, создавать отдельные рабочие ветки с помощью Git worktree и управлять задачами и терминалами.

## Что это даёт

- Общие инструкции и скиллы обновляются из одного места для всех подключённых проектов.
- Агент видит, какие проекты связаны, и может учитывать код и документацию соседнего репозитория при решении задачи.
- Для каждого проекта можно выбрать свои возможности: Changesets, браузерные инструменты, Figma и другие.
- Инструменты и агенты работают внутри Docker. Вы подключаете существующие репозитории со своим стеком и конфигурацией.

## Начать работу

Установите ADC и Orca одной командой для своей ОС.

macOS / Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/foxford/ai-devcontainer/main/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/foxford/ai-devcontainer/main/install.ps1 | iex
```

Откройте новый терминал: команда `adc` и автодополнение уже подключены. Подробности и поддерживаемые ОС описаны в [инструкции по установке](docs/installation.md).

Подключите проекты и запустите среду. Пути в примере замените на свои:

```sh
adc project add ~/Development/frontend --id frontend --capability pnpm-nx,changesets,frontend
adc project add ~/Development/backend --id backend --description 'API продукта'
adc project relate frontend backend
adc workspace start
```

Откройте Orca на компьютере и подключите сервер по pairing URL из `adc workspace logs`. В Orca появятся подключённые проекты; можно создавать worktree, открывать терминалы и запускать агентов. Логины и подписки агентов подключите внутри среды.

В среде установлены Claude Code, Codex, OpenCode, Hermes и DSH. Выберите нужного агента в Orca или запустите его CLI в терминале проекта.

## Возможности для проекта

Выберите возможности при подключении репозитория. В таблице `<path>` означает путь к проекту; несколько возможностей можно передать через запятую: `--capability pnpm-nx,changesets,frontend`.

| Возможность | Команда подключения | Что получает агент | Настройка MCP |
|---|---|---|---|
| `pnpm-nx` | `adc project add <path> --capability pnpm-nx` | Правила работы с pnpm/Nx и документ `MONOREPO.md` | Не требуется |
| `changesets` | `adc project add <path> --capability changesets` | Правила подготовки changeset, версий и релизов пакетов | Не требуется |
| `playwright` | `adc project add <path> --capability playwright` | Правила проектных e2e и браузер через Playwright MCP | Автоматически |
| `chrome-devtools` | `adc project add <path> --capability chrome-devtools` | Профилирование, анализ сети и производительности через Chrome DevTools MCP | Автоматически |
| `frontend` | `adc project add <path> --capability frontend` | Правила работы со скиллами фронтенда, Playwright MCP и Chrome DevTools MCP | Оба автоматически |
| `figma` | `adc project add <path> --capability figma` | Правила использования дизайна и Figma MCP | `adc mcp enable figma --project`, затем OAuth у агента |
| `graphify` | `adc project add <path> --capability graphify` | Полный скилл Graphify, CLI, локальный индекс кода и MCP для вопросов о связях | Автоматически; индекс обновляется ADC |
| `obsidian` | `adc project add <path> --capability obsidian` | Скилл работы с заметками и доступ к Obsidian MCP на компьютере | Настроить Local REST API в Obsidian, затем `adc mcp enable obsidian --project` |

Для уже подключённого проекта и общего выбора команды используйте [настройку возможностей](docs/workspace.md#возможности-и-проектные-настройки). ADC сохраняет инструкции и конфигурацию самого репозитория. Выбранные возможности описывают правила работы агента; зависимости приложения устанавливаются по правилам проекта.

## Включить и проверить MCP

Запускайте команды из каталога подключённого проекта. Для `playwright`, `chrome-devtools` и `frontend` ADC сам подготавливает браузер и доставляет конфигурацию при запуске среды или синхронизации.

```sh
cd ~/Development/frontend
adc mcp list
adc mcp sync
```

Для Figma выберите возможность `figma`, затем выполните:

```sh
adc mcp enable figma --project
```

На запрос `FIGMA_MCP_ENABLED` введите `1`, затем авторизуйтесь через MCP-интерфейс выбранного агента. Для Codex команда синхронизации выводит отдельную инструкцию по добавлению сервера и входу.

Для Obsidian выберите возможность `obsidian`, включите HTTP-доступ в плагине Local REST API на порту `27123`, затем выполните:

```sh
adc mcp enable obsidian --project
```

Команда запросит API-ключ из настроек плагина. Секреты сохраняются локально и не попадают в Git. Как добавить свой MCP и настроить его область действия, описано в [справочнике рабочей среды](docs/workspace.md#mcp-и-секреты).

## Повседневные команды

В примерах `frontend` обозначает ID подключённого проекта.

| Задача | Команда |
|---|---|
| Показать подключённые проекты | `adc project list` |
| Открыть терминал проекта в среде | `adc workspace shell frontend` |
| Создать worktree для задачи | `adc worktree create frontend fix-validation` |
| Обновить инструкции и инструменты проекта | `adc sync frontend` |
| Обновить инструкции всех проектов | `adc sync --all` |
| Добавить агентам источник знаний | `adc project knowledge frontend docs/architecture.md 'Команда frontend'` |
| Проверить состояние среды | `adc workspace status` |
| Остановить среду с сохранением данных | `adc workspace stop` |
| Проверить установку | `adc doctor` |
| Открыть справку по задачам Orca | `adc task --help` |
| Открыть справку по координации агентов | `adc orca orchestration --help` |
| Запустить панель OmniRoute | `adc omniroute serve` → http://localhost:20128 |
| Обновить граф кода текущего проекта | `adc tools graphify index` |
| Посмотреть все команды ADC | `adc --help` |

Новые подключения применяются командой `adc workspace start`. При изменении набора проектов контейнер пересоздаётся; сначала завершите или приостановите работу агентов. Исходники, worktree и сохранённые данные среды остаются на месте.

OmniRoute поставляется внутри среды как общий шлюз к моделям. Провайдеры подключаются в его панели; агенты используют шлюз после настройки их адреса API. Подробнее о Graphify и OmniRoute — в [справочнике среды](docs/workspace.md).

## Подробнее

- [Установка, обновление и автодополнение](docs/installation.md).
- [Знания, возможности, MCP, скиллы и миграция](docs/workspace.md).
- [Архитектура и разработка платформы](SPEC.md).
- [Доставляемые материалы и каталог возможностей](delivery/README.md).
