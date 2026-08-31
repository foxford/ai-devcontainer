---
name: "nx-monorepo"
description: "Оркестрация монорепо через Nx — команды, кэш, таргеты, project.json, теги, граф зависимостей, nx affected. Когда запускаешь задачи по пакетам или настраиваешь Nx."
slug: "nx-monorepo"
metadata:
  category: "tooling"
---

# Skill: Nx Monorepo — Оркестрация сборки

## Когда использовать

При запуске сборки/тестов/линтинга через Nx, настройке project.json, отладке проблем с кэшированием, работе с графом зависимостей.

## Паттерны и правила

### 1. Конфигурация Nx (nx.json)

```json
{
  "npmScope": "@foxford",
  "workspaceLayout": {
    "appsDir": "apps",
    "libsDir": "packages"
  },
  "targetDefaults": {
    "build": {
      "outputs": ["{projectRoot}/build"],
      "cache": true
    },
    "lint": {
      "dependsOn": ["^lint"],
      "outputs": ["{projectRoot}/.eslintcache"],
      "cache": true
    },
    "test": {
      "dependsOn": ["^test"],
      "cache": true
    },
    "dev": {
      "dependsOn": ["^build"]
    }
  },
  "defaultBase": "main"
}
```

### 2. Структура project.json

```json
// packages/<name>/project.json
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

Все Nx-таргеты делегируют в pnpm-скрипты.

### 3. Теги пакетов

| Тег           | Значение                    |
| ------------- | --------------------------- |
| `buildable`   | Пакет имеет этап сборки     |
| `lintable`    | Пакет линтится              |
| `publishable` | Пакет публикуется в npm     |
| `archivable`  | Пакет упаковывается в архив |

Фильтрация:

```bash
nx show projects --projects='tag:publishable'
nx run-many --target=build --projects='tag:publishable'
nx show projects --exclude='tag:publishable'
```

### 4. Основные команды

```bash
# Сборка одного пакета
nx build @foxford/sdk

# Сборка всех
nx run-many --target=build

# Линтинг одного
nx lint @foxford/sdk

# Тесты одного
nx test @foxford/sdk

# Граф зависимостей (визуализация)
nx graph

# Список проектов
nx show projects

# Affected (изменённые относительно main)
nx affected --target=build
nx affected --target=test
```

### 5. Кэширование

Кэшируемые операции: `build`, `lint`, `test`.

```bash
# Очистка кэша Nx
nx reset

# Или через pnpm
pnpm clean:cache
```

Кэш хранится в `.nx/cache/`. Outputs для сборки: `{projectRoot}/build`.

### 6. Зависимости между таргетами

```
dev  → зависит от → ^build (сначала собрать зависимости)
lint → зависит от → ^lint  (сначала линтить зависимости)
test → зависит от → ^test  (сначала тестировать зависимости)
```

`^` означает зависимости проекта (transitive).

### 7. Генераторы (automation)

```bash
# Создание нового пакета через Nx генератор
nx generate @foxford/automation:package --name=my-package --useTsup
```

Генератор создаёт: project.json, package.json, tsconfig.json, src/index.ts, README.mdx.

## Антипаттерны

- Запуск `pnpm run build` из корня — использовать `nx build @foxford/<name>`
- Ручное создание project.json — использовать генератор `@foxford/automation:package`
- Игнорирование кэша — `nx reset` только при реальных проблемах
- `nx run-many` без `--projects` фильтра для тяжёлых операций
- Прямые зависимости между пакетами без project.json

## Важно

- Nx версия: 22.5.3
- Базовая ветка: `main` (для affected-команд)
- `nx graph` — визуализация зависимостей в браузере
- Все таргеты используют `nx:run-commands` executor
- Changesets игнорируют внутренние непубликуемые пакеты (automation, env и приложения)
