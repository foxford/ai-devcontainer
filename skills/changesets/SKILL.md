---
name: "changesets"
description: "Версионирование и публикация пакетов npm через Changesets — bump версий, changelog, релизы, GitHub Action. Когда нужно завести changeset, поднять версию пакета или опубликовать в реестр."
slug: "changesets"
metadata:
  category: "ci-cd"
---

# Skill: Changesets — Версионирование и публикация

## Когда использовать
При настройке автоматической публикации пакетов, интеграции changesets с GitHub Actions, работе с версиями в монорепозитории.

## Паттерны и правила

### 1. Конфигурация проекта

```json
// .changeset/config.json
{
  "changelog": "@changesets/cli/changelog",
  "commit": false,
  "fixed": [],
  "linked": [],
  "access": "public",
  "baseBranch": "main",
  "updateInternalDependencies": "patch",
  "ignore": [
    "@foxford/automation"
  ]
}
```

**Ключевые настройки:**
- `access: "public"` — все пакеты публикуются как public
- `commit: false` — changesets не создаёт коммиты автоматически
- `baseBranch: "main"` — базовая ветка для определения изменений
- `ignore` — пакеты, которые НЕ участвуют в версионировании
- `updateInternalDependencies: "patch"` — при обновлении зависимости bumps patch

### 2. Скрипты в корневом package.json

```json
{
  "scripts": {
    "changeset": "changeset",
    "version-packages": "changeset version",
    "release": "changeset publish"
  }
}
```

| Команда | Что делает |
|---------|-----------|
| `pnpm changeset` | Интерактивно создаёт changeset-файл (.changeset/*.md) |
| `pnpm version-packages` | Применяет changesets: обновляет версии в package.json, генерирует CHANGELOG.md |
| `pnpm release` | Публикует пакеты с изменёнными версиями в npm |

### 3. GitHub Action: changesets/action

```yaml
# .github/workflows/release.yml
name: Release
on:
  push:
    branches: [main]

jobs:
  release:
    runs-on: nscloud-ubuntu-22.04-amd64-2x4
    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 0

      - uses: ./.github/actions/setup-node
      - uses: ./.github/actions/install-deps

      - name: Create Release PR or Publish
        uses: changesets/action@v1
        with:
          version: pnpm version-packages
          publish: pnpm release
          title: 'chore: version packages'
          commit: 'chore: version packages'
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          NPM_TOKEN: ${{ secrets.NPM_TOKEN }}
```

### 4. Flow публикации (двухфазный)

```
Разработчик                    GitHub Actions
     │                              │
     ├─ pnpm changeset              │
     ├─ commit + push to PR         │
     ├─ PR merged → main ──────────→│
     │                              ├─ changesets/action обнаруживает .changeset/*.md
     │                              ├─ Создаёт/обновляет PR "Version Packages"
     │                              │   (bumps versions, generates changelogs)
     │                              │
     ├─ Merge "Version Packages" ──→│
     │                              ├─ changesets/action: нет .changeset файлов
     │                              ├─ Запускает `pnpm release` (changeset publish)
     │                              └─ Пакеты опубликованы в npm
```

**Фаза 1 (есть changeset файлы):**
- Action создаёт/обновляет PR "Version Packages"
- В PR: обновлённые package.json, CHANGELOG.md, удалённые .changeset файлы

**Фаза 2 (нет changeset файлов = Version PR смержен):**
- Action запускает `pnpm release` → `changeset publish`
- Публикуются только пакеты с изменённой версией
- Пакеты из `ignore` не публикуются

### 5. NPM Token для публикации

```yaml
# Вариант 1: через env
env:
  NPM_TOKEN: ${{ secrets.NPM_TOKEN }}

# Вариант 2: через .npmrc
- name: Setup npm auth
  shell: bash
  run: echo "//registry.npmjs.org/:_authToken=${{ secrets.NPM_TOKEN }}" >> .npmrc
```

### 6. Changeset файл — формат

```markdown
---
'@foxford/sdk': minor
'@foxford/services': patch
---

Добавлена поддержка нового API метода в SDK.
Services: исправлена обработка ошибок retry.
```

- Заголовок: список пакетов с типом bump (major/minor/patch)
- Тело: описание изменений (попадает в CHANGELOG.md)
- Файл: `.changeset/<random-name>.md`

### 7. Публикация

`changeset publish` использует стандартный `npm publish`. Публикация выполняется автоматически через `changesets/action` в CI после мержа PR "Version Packages" — ручные деплой-скрипты не используются.

### 8. Игнорируемые пакеты

Changesets НЕ версионирует (пример):
- `@foxford/automation` — внутренний генератор
- `@foxford/env` — внутренний пакет окружения
- приложения (не npm-пакеты)

Эти пакеты можно менять без создания changeset файлов.

## Антипаттерны

- Публикация без changeset — версия не обновится, npm reject
- `changeset publish` для пакетов без тега `publishable` — будет пытаться опубликовать всё
- Ручной bumping версий в package.json — только через `changeset version`
- `commit: true` в config — может создать конфликты при автоматических коммитах
- Забытые changeset файлы — PR "Version Packages" будет накапливать изменения

## Важно

- Версия CLI: `@changesets/cli` 2.29.8
- Публикуются только пакеты с тегом `publishable`
- `changeset publish` публикует в npm registry
- `workspace:*` зависимости автоматически заменяются на реальные версии при публикации
- `updateInternalDependencies: "patch"` — зависимые пакеты получают patch bump
