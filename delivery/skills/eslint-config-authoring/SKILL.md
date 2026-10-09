---
name: "eslint-config-authoring"
description: "Создание и правка ESLint-конфигов — factory pattern, пресеты, кастомные плагины и правила. Когда меняешь @foxford/eslint-config или добавляешь новый пресет линтинга."
slug: "eslint-config-authoring"
metadata:
  category: "tooling"
---

# Skill: ESLint Config Authoring — Конфигурация ESLint

## Когда использовать
При изменении правил линтинга, добавлении кастомных правил, настройке ESLint для нового пакета, или отладке проблем с линтингом.

## Паттерны и правила

### 1. Factory pattern конфигурации

`@foxford/eslint-config` использует factory-функцию с параметром `react`:

```javascript
// packages/eslint-config/current/index.js
module.exports = ({ react }) => {
  process.env.FOXFORD_ESLINT_CONFIG_INJECT_REACT = react ? 'true' : 'false'

  const js = require('./languages/javascript')
  const ts = require('./languages/typescript')

  return {
    overrides: [...js, ...ts],
    rules: {},
    ignorePatterns: ['docs', 'build', 'out', '__test__', 'dist', '**/node_modules/**'],
  }
}
```

### 2. Четыре пресета

```javascript
// packages/eslint-config/index.js — без React
module.exports = require('./current')({ react: false })

// packages/eslint-config/react.js — с React
module.exports = require('./current')({ react: true })

// packages/eslint-config/node.js — для Node.js пакетов
module.exports = require('./node')

// packages/eslint-config/legacy.js — для старого кода
module.exports = require('./legacy')
```

Подключение в пакете:
```json
{
  "eslintConfig": {
    "extends": "@foxford/eslint-config/react"
  }
}
```

### 3. Раздельные парсеры для JS и TS

```javascript
// current/languages/javascript.js
{
  parser: '@babel/eslint-parser',
  files: ['*.js', '*.jsx'],
  env: { browser: true, es6: true, jest: true, node: true },
}

// current/languages/typescript.js
{
  parser: '@typescript-eslint/parser',
  files: ['*.ts', '*.tsx'],
  // enforces consistent type imports/exports
}
```

### 4. Структура правил

Правила агрегируются из модулей:

```
nodeRules → eslintRules → importRules → customRules → flowtypeRules
                                                    ↓ (если react: true)
                                              + reactRules + jsxRules
```

Каждый модуль — отдельный файл в `current/rules/`.

### 5. Кастомный плагин проекта

```
packages/eslint-config/plugins/custom/
├── index.js        # entry point
└── rules/          # кастомные правила
```

Подключается как `file:./plugins/custom` в dependencies.

### 6. Import resolver для монорепо

```javascript
{
  settings: {
    'import/resolver': {
      '@helljs/eslint-import-resolver-x': {}
    },
    'import/parsers': {
      '@babel/eslint-parser': ['.js', '.jsx'],
      '@typescript-eslint/parser': ['.ts', '.tsx'],
    },
  },
}
```

### 7. Стандартный скрипт линтинга

```json
{
  "lint:eslint": "eslint --cache --cache-strategy content --max-warnings=0 ./",
  "lint:type-check": "tsc --noEmit --pretty",
  "lint:clean": "rm -f .eslintcache"
}
```

- `--cache` — кэширование результатов
- `--cache-strategy content` — инвалидация по содержимому (не по mtime)
- `--max-warnings=0` — warnings = errors

## Антипаттерны

- Создание `.eslintrc` в пакете вместо использования `@foxford/eslint-config` пресета
- `// eslint-disable` без обоснования в комментарии
- Отключение правил в конфигурации пакета без согласования
- Использование `--no-cache` — кэш значительно ускоряет линтинг
- Забытый `lint:clean` при проблемах с кэшем

## Важно

- ESLint конфигурация — CommonJS (`module.exports`)
- Не путать: `index.js` (без React) vs `react.js` (с React)
- Print width 120 — согласовано с Prettier
- Ignore patterns: docs, build, out, __test__, dist, node_modules
- `max-warnings=0` — строгий режим, любой warning ломает CI
