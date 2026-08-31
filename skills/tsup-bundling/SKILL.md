---
name: "tsup-bundling"
description: "Сборка пакетов через tsup — двойной CJS+ESM выход, post-build хуки, трансформация package.json. Когда настраиваешь или чинишь сборку библиотеки."
slug: "tsup-bundling"
metadata:
  category: "build"
---

# Skill: tsup Bundling — Паттерны сборки пакетов

## Когда использовать
При настройке сборки нового пакета, модификации tsup.config.ts, или отладке проблем со сборкой.

## Паттерны и правила

### 1. Базовая конфигурация tsup

```typescript
// packages/<name>/tsup.config.ts
import { defineConfig } from 'tsup'

export default defineConfig({
  entry: ['src/index.ts'],
  format: ['cjs', 'esm'],
  dts: true,
  outDir: 'build',
  splitting: false,
  sourcemap: false,
  bundle: true,
  clean: true,
})
```

Выходные файлы: `build/index.cjs`, `build/index.mjs`, `build/index.d.ts`.

### 2. Post-build хук для package.json

Все publishable-пакеты трансформируют package.json в build/:

```typescript
import { defineConfig } from 'tsup'
import fs from 'fs-extra'
import path from 'path'

export default defineConfig({
  entry: ['src/index.ts'],
  format: ['cjs', 'esm'],
  dts: true,
  outDir: 'build',
  async onSuccess() {
    // 1. Копировать README
    await fs.copy('README.mdx', 'build/README.mdx')

    // 2. Трансформировать package.json
    const pkg = await fs.readJson('package.json')
    delete pkg.devDependencies
    delete pkg.scripts

    pkg.main = './index.cjs'
    pkg.module = './index.mjs'
    pkg.types = './index.d.ts'
    pkg.exports = {
      '.': {
        types: './index.d.ts',
        import: './index.mjs',
        require: './index.cjs',
      },
    }

    await fs.writeJson('build/package.json', pkg, { spaces: 2 })
  },
})
```

### 3. Множественные entry points (SDK паттерн)

SDK имеет основной и плагиновые entry points:

```typescript
// tsup.config.ts — основная сборка
export default defineConfig({
  entry: ['src/index.ts'],
  format: ['cjs', 'esm'],
  dts: true,
  outDir: 'build',
})

// tsup.config.plugins.ts — плагины
export default defineConfig({
  entry: {
    'plugins/autocomplete-email': 'src/plugins/autocomplete-email/index.ts',
    'plugins/confirm-phone': 'src/plugins/confirm-phone/index.ts',
  },
  format: ['cjs', 'esm'],
  dts: true,
  outDir: 'build',
})
```

```json
{
  "build": "pnpm run build:sdk && pnpm run build:plugins",
  "build:sdk": "tsup",
  "build:plugins": "tsup --config tsup.config.plugins.ts"
}
```

### 4. Удаление react из dependencies (i18n паттерн)

Пакеты с React peer-зависимостями удаляют react из build/package.json:

```typescript
async onSuccess() {
  const pkg = await fs.readJson('package.json')

  // Удалить react/react-dom — они peer
  delete pkg.dependencies?.react
  delete pkg.dependencies?.['react-dom']

  await fs.writeJson('build/package.json', pkg, { spaces: 2 })
}
```

### 5. Генерация Flow типов

Пакеты с Flow-поддержкой генерируют `.flow` файлы в post-build:

```json
{
  "postbuild": "dts-bundle-generator -o build/index.d.ts src/index.ts && flowgen build/index.d.ts -o build/index.js.flow"
}
```

### 6. Публикация

Публикация выполняется через changesets-флоу (`changeset publish` в CI). Публикуется содержимое `build/` (поле `files` в package.json) с трансформированным package.json.

### 7. Версия tsup и зависимости

```json
{
  "devDependencies": {
    "tsup": "catalog:dev",
    "dts-bundle-generator": "9.5.1",
    "flow-bin": "0.167.1",
    "flowgen": "1.21.0"
  }
}
```

`catalog:dev` — версия tsup (8.5.1) определена в `pnpm-workspace.yaml` catalogs.

## Антипаттерны

- Сборка без `dts: true` — типы нужны всегда
- Публикация package.json с devDependencies
- Забытый `onSuccess` — build/ без корректного package.json
- `sourcemap: true` в production-сборке (если не beta)
- Ручное управление clean — tsup делает это сам при `clean: true`

## Важно

- Выходная директория всегда `build/` (настроено в `nx.json`)
- Nx кэширует результат сборки по `{projectRoot}/build`
- `splitting: false` — единый бандл, без code splitting
- dts-bundle-generator используется для создания единого `.d.ts` файла
- Тег `publishable` в project.json обозначает пакеты для публикации
