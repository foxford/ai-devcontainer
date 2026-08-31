/**
 * Entry package: vitest-config"
 */
import { defineConfig } from 'vitest/config'

export default defineConfig({
  cacheDir: './node_modules/.cache/vite',
  test: {
    clearMocks: true,
    coverage: {
      provider: 'istanbul',
      reporter: ['json', 'json-summary'],
      reportsDirectory: './coverage',
    },
    env: {
      NODE_ENV: 'test',
    },
    environment: 'jsdom',
    exclude: [
      '**/node_modules/**',
      '**/dist/**',
      '**/build/**',
      '**/cypress/**',
      '**/.{idea,git,cache,output,temp}/**',
      '**/{karma,rollup,webpack,vite,vitest,jest,ava,babel,nyc,cypress,tsup,build,eslint,prettier}.config.*',
    ],
    globals: false,
    include: ['**/*.{test,spec}.?(c|m)[jt]s?(x)'],
    mockReset: true,
    // Дефолт по всему монорепо — `forks`: полная изоляция процессов, поведение
    // идентично локально и в CI. `vmThreads` (Node vm-контекст) быстрее, но в CI
    // ломается на инструментации istanbul + CJS-зависимостях (`vi.mock`-хойстинг,
    // fs.readFileSync → undefined и т.п.): «локально ок, в CI падает». Пакеты по
    // одному уже сбегали с него на forks — централизуем здесь.
    pool: 'forks',
    restoreMocks: true,
  },
})
