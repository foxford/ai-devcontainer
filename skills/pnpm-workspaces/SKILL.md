---
name: "pnpm-workspaces"
description: "Управление pnpm-воркспейсом — протокол workspace:*, catalogs, скрипты, публикация, связка с changesets. Когда работаешь с зависимостями между пакетами монорепо."
slug: "pnpm-workspaces"
metadata:
  category: "tooling"
---

# Skill: pnpm Workspaces — Управление зависимостями

## Когда использовать
При добавлении/обновлении зависимостей, настройке workspace-ссылок, работе с changesets, публикации пакетов.

## Паттерны и правила

### 1. Конфигурация workspace

```yaml
# pnpm-workspace.yaml
packages:
  - apps/*
  - packages/*

catalogs:
  dev:
    glob: ^13.0.6
    typescript: ^5.9.3
    zx: ^8.8.5
    tsup: ^8.5.1
    fs-extra: ^11.3.3

ignoredBuiltDependencies:
  - core-js
  - core-js-pure
  - esbuild
  - final-form
  - protobufjs

onlyBuiltDependencies:
  - nx
```

### 2. Внутренние зависимости — workspace:*

```json
{
  "devDependencies": {
    "@foxford/eslint-config": "workspace:*",
    "@foxford/typescript-config": "workspace:*",
    "@foxford/vitest-config": "workspace:*",
    "@foxford/prettier-config": "workspace:*"
  },
  "dependencies": {
    "@foxford/services": "workspace:*",
    "@foxford/types": "workspace:*"
  }
}
```

`workspace:*` — всегда текущая локальная версия. При публикации pnpm заменяет на реальную версию.

### 3. Catalogs для общих dev-зависимостей

```json
{
  "devDependencies": {
    "typescript": "catalog:dev",
    "tsup": "catalog:dev",
    "glob": "catalog:dev"
  }
}
```

`catalog:dev` — версия берётся из `pnpm-workspace.yaml` секции `catalogs.dev`. Обеспечивает единую версию во всех пакетах.

### 4. Скрипты секционирования

Все пакеты используют comment-секции в scripts:

```json
{
  "scripts": {
    "============================ BUILD =============================": "",
    "build": "tsup",
    "============================ LINT ==============================": "",
    "lint:clean": "rm -f .eslintcache",
    "lint:type-check": "tsc --noEmit --pretty",
    "lint:eslint": "eslint --cache --cache-strategy content --max-warnings=0 ./",
    "============================ TEST ==============================": "",
    "test": "vitest run",
    "test:watch": "vitest"
  }
}
```

### 5. Changesets — версионирование

```bash
# Создать changeset (описание изменений)
pnpm changeset

# Применить версии
pnpm version-packages

# Опубликовать
pnpm release
```

Конфигурация `.changeset/config.json`:
```json
{
  "access": "public",
  "baseBranch": "main",
  "commit": false,
  "updateInternalDependencies": "patch",
  "ignore": ["@foxford/automation"]
}
```

### 6. Установка зависимостей

```bash
# Установить все зависимости
pnpm install

# Добавить зависимость в пакет
cd packages/<name>
pnpm add <package>

# Добавить dev-зависимость
pnpm add -D <package>

# Добавить workspace-зависимость
pnpm add @foxford/<other-package>@workspace:*
```

### 7. npmrc настройки

```ini
# .npmrc
save-exact=true        # Точные версии (без ^/~)
engine-strict=false    # Не проверять engines
```

### 8. Очистка

```bash
pnpm clean           # Параллельная очистка: кэш + deps + build
pnpm clean:hard      # pnpm store prune + clean
pnpm clean:build     # Удалить build/ и dist/ во всех пакетах
pnpm clean:deps      # Удалить node_modules везде
pnpm clean:cache     # Сбросить кэш Nx + .cache + .eslintcache
```

## Антипаттерны

- `npm install` или `yarn` — только pnpm
- Версии с `^` или `~` — `save-exact=true` требует точные версии
- Прямое указание версий для catalog-зависимостей — использовать `catalog:dev`
- `pnpm add` из корня для пакетной зависимости — выполнять из директории пакета

## Важно

- **pnpm версия**: 10.30.3 (зафиксирована в `packageManager` field)
- **ОГРАНИЧЕНИЕ**: установка любых пакетов требует разрешения
- `save-exact=true` — все версии точные
- `workspace:*` при публикации заменяется на реальную версию
- Catalogs обеспечивают единство версий dev-зависимостей
