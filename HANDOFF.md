# HANDOFF — контекст разработки ai-devcontainer

Файл для передачи контекста между сессиями/агентами. Прочитай перед работой
над платформой. Актуален на 2026-07-23.

## Что это и откуда выросло

Платформа выросла из починки девконтейнера **bunker** (гулял юзер node/vscode,
маунты в чужой $HOME, EACCES от root-каталогов, corepack исчез в Node 25+) и
переноса опыта продакшн-монорепо **frontend-platform** (Foxford). Изначально
весь опыт был перенесён и очищен от брендинга (`grep -ri foxford .` должен
был быть пуст) — платформа задумывалась как личный проект автора. Позже это
решение пересмотрено: теперь это внутренний инструмент компании foxford,
публичный репозиторий `github.com/foxford/ai-devcontainer`, упоминания
foxford — часть бренда, а не то, что нужно вычищать.

## Ключевые архитектурные решения (и почему)

1. **Без registry**: `install.sh` (curl | bash) ставит клон в
   `~/.local/share/ai-devcontainer` + CLI `bin/adc` в `~/.local/bin`.
   Образ `dev-base:local` собирается ЛОКАЛЬНО; ключ актуальности — хеш
   Dockerfile+tooling/setup.sh (label `aidevcontainer.key`), НЕ git-rev.
2. **В образ печётся только `tooling/setup.sh`** (нужен при build проектного
   образа). Остальные скрипты/сиды монтируются в контейнеры read-only:
   `~/.local/share/ai-devcontainer → /opt/ai-devcontainer`. Правка скриптов/скиллов
   не требует пересборки образа.
3. **skeleton/ — единственный источник правды о проекте**: монорепа Nx+pnpm
   catalogs+changesets, пакеты `@foxford/*` (копией, НЕ публикуются),
   скиллы платформы `skills/` (overlay, не копируются в проект), `.hermes` (6 ролей), AGENTS/CLAUDE/MONOREPO.md,
   тонкий `.devcontainer` (FROM dev-base:local + initializeCommand).
4. **Персист per-project**: `~/.ai-devcontainer-dev/<проект>/{claude,codex,hermes,
   dsh,opencode,uv,history}` — создаёт initializeCommand проекта (не docker → не root).
   Общие только volumes `platform-ai-tools`, `platform-pnpm-store`.
5. **Obsidian на хосте** — два независимых канала: файловый vault-персист
   (`obsidian-vault`-маунт, для `graphify --obsidian`) и MCP-сервер плагина
   Local REST API через `host.docker.internal` (`README.md, раздел «Obsidian»`).
   Изначально был кастомный unix-socket демон (`.devcontainer/obsidian`) —
   заменён на готовый плагин, когда выяснилось, что он есть и делает то же
   самое без самодельного протокола.
6. **debian-slim + asdf** (не node-образ): `.tool-versions` — единый источник
   версии ноды; Node 25+ без corepack → `setup.sh` ставит его `npm i -g`.
7. **MCP-серверы — тот же overlay, что скиллы**: платформенный слой
   `mcp/servers.json` + проектный `<repo>/.agents/mcp.json`, сборка
   `tooling/wire-mcp.sh`. Раскладывается в ТРИ места, потому что проектный
   скоуп для MCP есть только у Claude (`.mcp.json`); Codex и Hermes держат
   серверы в home, а он в платформе пер-проектный — коллизий нет.
   Стоковые: `playwright`, `chrome-devtools`, `figma`.
   Секреты — `.agents/mcp.secrets.env` (в .gitignore, 600), раскрывает
   wire-mcp: `${VAR}` умеют Claude и Hermes, на Codex не проверено, а
   половинчатая схема хуже честной. `x-requires` (`env:`/`path:`) гейтит
   раздачу — слой платформы общий, и сервер без токена не должен доезжать
   до проекта, где он всё равно упадёт.
8. **Strix — не MCP, а CLI** (`uv tool install strix-agent`, как graphify,
   в volume /opt/ai-tools). Автономные пентест-агенты в docker-песочнице,
   свой LLM-ключ, свой биллинг. Как пользоваться — в скилле `senior-security`.
9. **Браузеры Playwright — в named volume, не в образе**: ревизия браузера
   привязана к версии пакета, образ общий. В образе только apt-часть.
   Ревизий в volume две — у раннера (`@playwright/test`) и у MCP
   (`@playwright/mcp` тянет свой playwright) версии разные.

## Грабли, на которые уже наступали (не повторять)

- `--passWithNoTests` в комбинированном `nx run-many -t lint,test` раздаётся
  ВСЕМ таргетам → eslint/tsc падают. Только отдельным вызовом test.
- Устаревшие `.eslintcache`/`.nx` после переезда каталогов дают ложные фейлы —
  чистить перед диагностикой.
- eslint-config: понадобился явный dep `eslint-import-resolver-node` и
  `const rule = {...}; export default rule` (TS2742 при declaration-emit).
- prettier резолвится от корня монорепы — в skeleton есть корневой
  `prettier.config.js`, реэкспорт из `@foxford/prettier-config`.
- `initializeCommand` — несущий элемент: гарантирует dev-base:local до FROM.
- «Rebuild Container Without Cache» в VS Code ломает FROM локального образа
  (флаг --pull → pull access denied). Только обычный Rebuild; с нуля —
  `docker rmi dev-base:local && adc ensure-image`.
- Образы, собранные в dind-контейнере (bunker), живут в его внутреннем демоне —
  на хосте базу собирает первый `ensure-image` хостовым докером.
- Девконтейнер платформы собирает базу из РАБОЧЕЙ КОПИИ
  (`AI_DEVCONTAINER_HOME=$PWD`), проектные — из установленного клона.
- **`codex mcp add --url` КОННЕКТИТСЯ к серверу прямо при добавлении** и на
  OAuth-сервере поднимает интерактивный запрос авторизации. В postCreate это
  висяк, а у человека — промпт «авторизуйте MCP» на ровном месте (наступили в
  живую, на figma). Та же болезнь, что у `hermes mcp add`. Вывод шире одного
  флага: **OAuth-сервер вообще нельзя раздавать без явного согласия** — попав
  в конфиг, он просит логин при КАЖДОМ старте агента во всех проектах. Отсюда
  `x-requires: env:FIGMA_MCP_ENABLED` и отказ добавлять url-серверы в Codex.
- **Каталог под named volume обязан быть предсоздан в образе.** Проверено на
  пробе: volume на несуществующий путь монтируется `root:root`, и юзер node в
  него не пишет. Раньше это знали про bind-маунты, но верно и для volumes.
- В `/opt/ai-tools` у claude лежит только **лаунчер**; сам бинарь (324 МБ) — в
  `~/.local/share/claude/versions/`. Пока этот путь не был примонтирован, каждый
  rebuild ронял лаунчер и `install-ai-tools.sh` качал 324 МБ заново. Лечится
  volume `platform-claude-versions`; `doctor` теперь про него спрашивает.
- `@playwright/mcp` по умолчанию идёт в канал **Google Chrome**, а не в
  встроенный chromium: без `--browser chromium` падает с «Chromium
  distribution 'chrome' is not found at /opt/google/chrome/chrome». Выглядит
  как «браузер не установлен», хотя установлен нужный.
- `hermes mcp add` **интерактивен** (спрашивает y/N, если сервер не ответил) и
  коннектится к серверу прямо при добавлении → в postCreate без TTY это висяк.
  Пишем `mcp_servers` в `~/.hermes/config.yaml` напрямую через yq.
- У пакета `playwright` в `exports` нет `./cli.js` → `require.resolve(
  'playwright/cli.js')` не работает. Поставить браузер нужной MCP-серверу
  версии: `npx -y --package=@playwright/mcp@<пин> playwright install chromium`
  (npx линкует бины и зависимостей пакета, не только его собственные).
- Пресет `@foxford/typescript-config` построен на `${configDir}` — он равен
  каталогу КОНФИГА. Для tsconfig вне пакета (корневой `e2e/`) это ломает
  `typeRoots` и `rootDir`; оба переопределяются явно, см. `skeleton/e2e/tsconfig.json`.
- Списки исключений в `adc new` — ручные: артефакты новых инструментов
  (`test-results/`, `playwright-report/`) уезжали в свежий проект, пока их туда
  не добавили. Заводишь инструмент с выхлопом в корень — правь и `cmd_new`.

## Состояние (все проверки — реальными прогонами)

- skeleton: lint/type-check/test/build — зелёные (в т.ч. 225 тестов eslint-config).
- `adc new` → проект собирается и проходит все таргеты с --skip-nx-cache.
- Образы: dev-base:local (слим) и девконтейнеры проекта/платформы собираются.
- Playwright: `pnpm run e2e` в skeleton зелёный на реальном chromium из volume;
  `e2e:type-check` чистый. Браузерный MCP поднят по stdio и проверен вживую —
  initialize, 24 инструмента, `browser_navigate` на localhost, снапшот,
  скриншот в `.playwright-mcp/`, чтение консоли страницы.
- `wire-mcp.sh` проверен на песочном репо: раздача в три конфига, повторный
  прогон идемпотентен, перекрытие `env` проектом, `null` = выключение,
  чистка уехавших серверов из всех трёх мест, отказ при битом JSON слоя.
- Секреты и гейт: значение с пробелами и `=` раскрывается верно, комментарии и
  `export ` в .env игнорируются, `.mcp.json` и secrets-файл получают 600.
  По умолчанию раздаётся ОДИН playwright — остальные три отсекаются
  (нет браузера / нет токена / OAuth не включён).
- `chrome-devtools-mcp@1.7.0` проверен вживую: 29 инструментов, навигация на
  реальный сайт через chromium из playwright-volume (`--executablePath`).
- Strix 1.5.3 ставится через `uv tool install strix-agent` и запускается
  (`strix --version`). Реального прогона сканирования НЕ делали — нужен свой
  LLM-ключ и цель, на которую есть авторизация.

## Открытые хвосты

1. ~~`install.sh`: URL репозитория — плейсхолдер; вписать реальный хост~~ —
   закрыто: `git@github.com:foxford/ai-devcontainer.git`, репозиторий
   публичный (нужно для `curl | bash` без авторизации).
2. Запушить ai-devcontainer на GitHub (`github.com/foxford/ai-devcontainer`,
   публичный); CI — перенести с `.gitlab-ci.yml` на GitHub Actions.
3. Перевести bunker/meter-reporter/milur307 на платформенные артефакты
   (тонкий .devcontainer из skeleton; персист мигрировать:
   `cp -a ~/.bunker-dev/claude ~/.ai-devcontainer-dev/bunker/claude` и т.д. —
   тогда история Claude-сессий проекта сохраняется).
4. Foxford-специфичные скиллы (framework-architecture, services-http,
   docusaurus, vike, react-components, github-actions) не переносились —
   решить, нужны ли аналоги под наш стек (gitlab-ci скилл вместо github-actions?).
5. `.hermes/bootstrap.sh` шаг сборки требует авторизованного hermes CLI —
   на чистой машине шаг честно скипается с подсказкой.
6. Возможный сахар: `tooling/sync-platform-packages.sh` — полуавтоматический
   перенос дифа конфиг-пакетов в существующие проекты (обсуждалось, не делалось).
