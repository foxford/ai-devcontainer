---
name: "package-scaffold"
description: "Создание нового пакета в монорепо — структура каталогов, конфиги, project.json, скрипты, exports. Когда заводишь новый пакет монорепо с нуля."
slug: "package-scaffold"
metadata:
  category: "development"
---

# Skill: Package Scaffold — Создание нового пакета

## Когда использовать
При создании нового пакета в монорепо, настройке его структуры, или проверке соответствия конвенциям.

## Паттерны и правила

### 1. Использование Nx-генератора

Предпочтительный способ — через генератор:

```bash
nx generate @foxford/automation:package --name=my-package --useTsup
```

Опции:
- `--name` — имя пакета (без scope)
- `--useTsup` — добавить tsup для сборки

### 2. Структура пакета с tsup (buildable)

```
packages/<name>/
├── src/
│   └── index.ts           # entry point
├── tests/                  # опционально, тесты могут быть рядом с src
├── package.json
├── project.json
├── tsconfig.json
├── tsup.config.ts
├── vitest.config.ts
└── README.mdx
```

### 3. Структура пакета без сборки (source-based)

```
packages/<name>/
├── src/
│   └── index.ts           # entry point (потребляется напрямую)
├── package.json
├── project.json
├── tsconfig.json
├── vitest.config.ts        # если есть тесты
└── README.mdx
```

### 4. package.json шаблон (buildable)

```json
{
  "name": "@foxford/<name>",
  "version": "1.0.0",
  "description": "",
  "type": "module",
  "main": "src/index.ts",
  "exports": {
    ".": "./src/index.ts"
  },
  "files": ["build"],
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
  },
  "devDependencies": {
    "@foxford/eslint-config": "workspace:*",
    "@foxford/prettier-config": "workspace:*",
    "@foxford/typescript-config": "workspace:*",
    "@foxford/vitest-config": "workspace:*",
    "tsup": "catalog:dev",
    "typescript": "catalog:dev"
  }
}
```

### 5. package.json шаблон (source-based)

```json
{
  "name": "@foxford/<name>",
  "version": "1.0.0",
  "type": "module",
  "main": "src/index.ts",
  "exports": {
    ".": "./src/index.ts"
  },
  "scripts": {
    "============================ BUILD =============================": "",
    "build": "echo \"No build specified\" && exit 0",
    "============================ LINT ==============================": "",
    "lint:clean": "rm -f .eslintcache",
    "lint:type-check": "tsc --noEmit --pretty",
    "lint:eslint": "eslint --cache --cache-strategy content --max-warnings=0 ./",
    "============================ TEST ==============================": "",
    "test": "vitest run",
    "test:watch": "vitest"
  },
  "devDependencies": {
    "@foxford/eslint-config": "workspace:*",
    "@foxford/prettier-config": "workspace:*",
    "@foxford/typescript-config": "workspace:*",
    "@foxford/vitest-config": "workspace:*",
    "typescript": "catalog:dev"
  }
}
```

### 6. project.json шаблон

```json
{
  "name": "@foxford/<name>",
  "sourceRoot": "{projectRoot}/src",
  "projectType": "library",
  "targets": {
    "build": {
      "executor": "nx:run-commands",
      "command": "pnpm run build"
    },
    "lint": {
      "executor": "nx:run-commands",
      "command": "pnpm run lint:eslint"
    }
  },
  "tags": ["buildable", "lintable", "publishable"]
}
```

Для source-based пакетов убрать `"buildable"` и `"publishable"` из tags.

### 7. tsconfig.json шаблон

```json
{
  "extends": "@foxford/typescript-config/tsconfig.base.json",
  "compilerOptions": {
    "outDir": "build"
  },
  "include": ["src"],
  "exclude": ["node_modules", "build", "dist"]
}
```

### 8. vitest.config.ts шаблон

```typescript
import { mergeConfig } from 'vitest/config'
import baseConfig from '@foxford/vitest-config'

export default mergeConfig(baseConfig, {})
```

## Антипаттерны

- Создание пакета вручную без генератора (если генератор подходит)
- Отсутствие project.json — Nx не увидит пакет
- Нестандартные имена скриптов — следовать шаблону секций
- Забытые config-пакеты в devDependencies
- `"type": "commonjs"` — предпочитать `"module"` для новых пакетов
- Отсутствие README.mdx — нужен для документации пакета

## Важно

- Scope: `@foxford/`
- Директория: `packages/<name>/`
- Публикация — через changesets-флоу (см. skill `changesets`)
- После создания: `pnpm install` (запускается генератором)
- Все config-пакеты подключаются через `workspace:*`
