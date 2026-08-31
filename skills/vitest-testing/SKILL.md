---
name: "vitest-testing"
description: "Юнит- и компонентные тесты на Vitest — shared config, моки, dual-mode, jsdom. Когда пишешь или настраиваешь unit-тесты в монорепо."
slug: "vitest-testing"
metadata:
  category: "testing"
---

# Skill: Vitest Testing — Паттерны тестирования

## Когда использовать
При написании, настройке или отладке тестов в любом пакете монорепо.

## Паттерны и правила

### 1. Наследование от shared config через mergeConfig

Каждый пакет с тестами наследует базовую конфигурацию:

```typescript
// packages/<name>/vitest.config.ts
import { mergeConfig } from 'vitest/config'
import baseConfig from '@foxford/vitest-config'

export default mergeConfig(baseConfig, {
  // пакетные переопределения (если нужны)
})
```

Базовая конфигурация (`@foxford/vitest-config`):
- environment: `jsdom`
- pool: `vmThreads`
- clearMocks, mockReset, restoreMocks: `true`
- coverage provider: `istanbul`
- include: `**/*.{test,spec}.?(c|m)[jt]s?(x)`

### 2. Стандартные скрипты для тестов

```json
{
  "test": "vitest run",
  "test:watch": "vitest"
}
```

### 3. Именование тестовых файлов

Файлы тестов размещаются рядом с исходным кодом или в директории `tests/`:

```
packages/<name>/
├── src/
│   ├── module.ts
│   └── module.test.ts          # рядом с модулем
├── tests/
│   └── integration.test.ts     # интеграционные тесты
└── vitest.config.ts
```

Паттерн именования: `<module>.test.ts` или `<module>.spec.ts`.

### 4. Моки автоматически сбрасываются

Благодаря базовой конфигурации (`clearMocks`, `mockReset`, `restoreMocks`) — моки очищаются перед каждым тестом. Не нужно вызывать `vi.clearAllMocks()` вручную.

```typescript
// ✅ Правильно — моки сбрасываются автоматически
import { describe, it, expect, vi } from 'vitest'

describe('UserService', () => {
  it('should fetch user', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ id: 1 })
    // ...
  })

  it('should handle error', async () => {
    // mockFetch уже сброшен — не нужно clearAllMocks
    const mockFetch = vi.fn().mockRejectedValue(new Error('fail'))
    // ...
  })
})
```

### 5. Dual-mode тестирование (logger)

Пакет `logger` имеет отдельные режимы для browser и node:

```typescript
// packages/logger/vitest.config.ts
import { mergeConfig } from 'vitest/config'
import baseConfig from '@foxford/vitest-config'

const mode = process.env.VITEST_MODE // 'browser' | 'node'

export default mergeConfig(baseConfig, {
  test: {
    include: mode === 'browser'
      ? ['src/**/*.browser.test.ts']
      : ['src/**/*.node.test.ts'],
    cache: {
      dir: mode === 'browser' ? '.vitest-browser' : '.vitest-node',
    },
  },
})
```

```json
{
  "test": "pnpm run test:node && pnpm run test:browser",
  "test:node": "vitest run --mode node",
  "test:browser": "vitest run --mode browser"
}
```

### 6. Запуск одного файла

```bash
# Из корня монорепо
npx vitest run packages/<name>/src/module.test.ts

# Из директории пакета
npx vitest run src/module.test.ts

# С фильтром по имени теста
npx vitest run -t "should fetch user"
```

### 7. Импорты из vitest, не глобальные

Globals отключены — всё импортируется явно:

```typescript
// ✅ Правильно
import { describe, it, expect, vi, beforeEach } from 'vitest'

// ❌ Неправильно — globals не настроены
describe('test', () => { ... }) // describe is not defined
```

## Антипаттерны

- Ручной `vi.clearAllMocks()` в `beforeEach` — уже настроено в базе
- Глобальные `describe`/`it` без импорта
- Создание `vitest.config.ts` с нуля без `mergeConfig` от базового
- Тяжёлые setup-файлы — предпочитать фабрики внутри тестов
- `test.only` / `describe.only` в коммитах

## Важно

- Pool `vmThreads` — тесты выполняются параллельно в VM-потоках
- Кэш Vitest: `./node_modules/.cache/vite`
- Тесты исключают: `node_modules`, `dist`, `build`, `cypress`, конфиги
- Nx кэширует результаты тестов (`"test": { "cache": true }`)
