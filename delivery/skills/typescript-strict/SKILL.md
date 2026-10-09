---
name: "typescript-strict"
description: "Строгий TypeScript в монорепо — конфигурация, алиасы путей, type-only импорты. Когда правишь tsconfig, типы или борешься с ошибками компиляции."
slug: "typescript-strict"
metadata:
  category: "language"
---

# Skill: TypeScript Strict — Паттерны строгого TypeScript

## Когда использовать
При написании или ревью любого TypeScript-кода в монорепо. При создании нового пакета или настройке tsconfig.

## Паттерны и правила

### 1. Наследование tsconfig от базового

Каждый пакет наследует конфигурацию из `@foxford/typescript-config`:

```json
// packages/<name>/tsconfig.json
{
  "extends": "@foxford/typescript-config/tsconfig.base.json",
  "compilerOptions": {
    "outDir": "build"
  },
  "include": ["src"],
  "exclude": ["node_modules", "build", "dist"]
}
```

Базовая конфигурация: `target: ES2016`, `module: Preserve`, `jsx: preserve`, `strict: true`, `moduleResolution: bundler`.

### 2. Path alias `~/*` → `src/*`

Все импорты внутри пакета используют алиас `~/`:

```typescript
// ✅ Правильно
import { UserService } from '~/services/user'
import type { CartItem } from '~/types/cart'

// ❌ Неправильно — относительные пути
import { UserService } from '../../services/user'
import type { CartItem } from '../../../types/cart'
```

Алиас настроен в `tsconfig.base.json`:
```json
"paths": {
  "~/*": ["${configDir}/src/*"]
}
```

### 3. Type-only импорты и экспорты

Типы ВСЕГДА импортируются через `import type` (enforced ESLint + `verbatimModuleSyntax`):

```typescript
// ✅ Правильно — разделение type и value импортов
import { createStore, createEvent } from 'effector'
import type { Store, Event } from 'effector'

// ✅ Правильно — type-only экспорт
export type { UserData, UserRole }
export { UserService }

// ❌ Неправильно — смешивание type и value
import { createStore, Store } from 'effector'
```

### 4. Именованные экспорты, никаких default

Все пакеты используют только именованные экспорты:

```typescript
// ✅ Правильно
export { Client } from './client'
export { Analytics } from './analytics'
export type { AnalyticsConfig } from './analytics'

// ❌ Неправильно — default export
export default class Client { ... }
```

### 5. Barrel-файлы в `src/index.ts`

Каждый пакет экспортирует API через единый entry point:

```typescript
// packages/<name>/src/index.ts
export { createComplex } from './complex'
export type { ComplexApi } from './complex'

export { createScalar } from './scalar'
export type { ScalarApi } from './scalar'

export { createFilter } from './filter'
export type { FilterApi } from './filter'
```

Паттерн: value-экспорт + type-экспорт рядом для каждого модуля.

### 6. Triple-slash references для ambient types

Для пакетов с кастомными типизациями используются triple-slash директивы:

```typescript
// packages/sdk/src/index.ts
/// <reference path="../types/camelcase-keys-deep.d.ts" />
/// <reference path="../types/decamelize-keys-deep.d.ts" />
```

### 7. Строгие проверки всегда включены

Не отключать strict-опции в пакетных tsconfig:

```typescript
// Включено в base: strict, noUnusedLocals, noUnusedParameters,
// noFallthroughCasesInSwitch, noUncheckedIndexedAccess
// Также: declaration, declarationMap, incremental

// ❌ Неправильно — отключение strict-опций
{
  "compilerOptions": {
    "strict": false,
    "noUnusedLocals": false
  }
}
```

## Антипаттерны

- `any` — использовать `unknown` + type guard вместо `any`
- Относительные пути `../../` — использовать `~/` алиас
- `import type` вместе с value-импортами в одной строке
- Default exports
- Отключение strict-опций в пакетном tsconfig
- `as` type assertions без обоснования — предпочитать type guards

## Важно

- `moduleResolution: bundler` — импорты без расширений (`.ts` не нужен)
- `verbatimModuleSyntax` — type-only импорты обязательны
- `incremental: true` — ускоряет повторную компиляцию
- Проверка типов: `tsc --noEmit --pretty` (не генерирует файлы)
