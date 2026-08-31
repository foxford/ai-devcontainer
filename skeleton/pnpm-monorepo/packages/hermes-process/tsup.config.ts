import { copyFile, rename } from 'node:fs/promises'

import { defineConfig } from 'tsup'

/**
 * Единая сборка пакета: два независимых таргета в один общий build/.
 *
 *   build/
 *     state-machine/index.js                     ← Node-бандл (gateway-хук)
 *     plugins/profile-skills/dashboard/
 *       manifest.json, plugin_api.py, dist/index.js ← плагин дашборда
 *
 * Внутренний dashboard/dist/index.js — это контракт дашборда: на него ссылается
 * manifest.json (entry). Весь build/ самодостаточен и целиком устанавливается
 * через .hermes/bootstrap.sh.
 */
export default defineConfig([
  // ── Таргет A: Node state machine ───────────────────────────────────────────
  {
    bundle: true,
    // clean выключен у обоих таргетов: tsup чистит только собственный outDir
    // конкретного конфига, поэтому весь build/ чистим в build-скрипте (rm -rf build).
    clean: false,
    entry: ['src/state-machine/index.ts'],
    // better-sqlite3 — native module; не бандлим, оставляем как external require.
    external: ['better-sqlite3'],
    format: ['esm'],
    minify: false,
    outDir: 'build/state-machine',
    // Output an .js that Node.js can run directly via `node build/state-machine/index.js`.
    outExtension: () => ({ js: '.js' }),
    platform: 'node',
    sourcemap: true,
    splitting: false,
    target: 'node20',
  },

  // ── Таргет B: UI-бандл дашборд-плагина ──────────────────────────────────────
  //
  // Это НЕ обычная библиотека: на выходе один IIFE для браузера, который берёт
  // React из window.__HERMES_PLUGIN_SDK__ (свой React не бандлим) и регистрирует
  // страницу. JSX компилируем классическим runtime в `React.createElement`.
  {
    bundle: true,
    clean: false,
    dts: false,
    entry: { index: 'src/plugins/profile-skills/ui/index.tsx' },
    esbuildOptions(options) {
      options.jsx = 'transform'
      options.jsxFactory = 'React.createElement'
      options.jsxFragment = 'React.Fragment'
    },
    format: ['iife'],
    minify: true,
    // tsup для format:iife пишет `<name>.global.js`. Приводим к dist/index.js
    // (как ждёт manifest.entry) и докладываем рядом статику плагина — manifest и
    // python-бэкенд, — чтобы build/plugins/profile-skills/dashboard/ был полным.
    async onSuccess() {
      const dashboard = 'build/plugins/profile-skills/dashboard'
      await rename(`${dashboard}/dist/index.global.js`, `${dashboard}/dist/index.js`).catch(() => undefined)
      await copyFile('src/plugins/profile-skills/manifest.json', `${dashboard}/manifest.json`)
      await copyFile('src/plugins/profile-skills/plugin_api.py', `${dashboard}/plugin_api.py`)
    },
    outDir: 'build/plugins/profile-skills/dashboard/dist',
    platform: 'browser',
    sourcemap: false,
    splitting: false,
    target: 'es2020',
  },
])
