import { defineConfig, devices } from '@playwright/test'

/**
 * Конфиг e2e-раннера. Тесты лежат в корневом `e2e/` — это НЕ пакет монорепы:
 * своего project.json у него нет, в nx-граф и pnpm-воркспейс он не входит.
 * Гоняется из корня: `pnpm run e2e`.
 *
 * Браузеры живут в volume ($PLAYWRIGHT_BROWSERS_PATH, задан в образе dev-base),
 * ставит их post-create; вручную — `pnpm run e2e:install`.
 *
 * Раннер НЕ входит в quality gates (`pnpm test` — это vitest): e2e требует
 * поднятого приложения и в общий прогон по всем пакетам не лезет.
 */
export default defineConfig({
  testDir: './e2e',
  outputDir: './test-results',

  // Прогон файлов параллельно; внутри файла тесты идут по порядку.
  fullyParallel: true,

  // .only, забытый в коммите, зелёным CI притворяться не должен.
  forbidOnly: Boolean(process.env.CI),

  // Ретрай — только в CI и только один: локально падение надо видеть сразу,
  // а не прятать за повтором. Ретраи в CI нужны, чтобы отличить реально
  // сломанный тест от разово моргнувшего окружения — но повтор, ставший
  // нормой, это баг теста, см. скилл playwright-pro (reference/flaky-tests.md).
  retries: process.env.CI ? 1 : 0,
  workers: process.env.CI ? 1 : undefined,

  // html-репорт не открываем сам: в контейнере некому — дисплея нет.
  // Смотреть: `pnpm run e2e:report` (поднимет сервер, порт пробросит VS Code).
  reporter: [['list'], ['html', { open: 'never' }]],

  use: {
    baseURL: process.env.E2E_BASE_URL ?? 'http://localhost:3000',

    // Артефакты только по факту падения — иначе прогон распухает на ровном месте.
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',

    // Тот же атрибут, что у браузерного MCP по умолчанию: getByTestId в тестах
    // и снапшот агента должны указывать на один и тот же элемент.
    testIdAttribute: 'data-testid',
  },

  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        launchOptions: {
          // Песочнице chromium нужны user namespaces, которых в контейнере
          // обычно нет: без этого флага браузер падает на старте с невнятным
          // "Target closed". Контейнер сам по себе и есть граница изоляции.
          args: ['--no-sandbox'],
        },
      },
    },
    // firefox и webkit — по надобности: каждый добавляет свой браузер в volume
    // (~1 ГБ на двоих) и своё время прогона. Не включай «на всякий случай».
  ],

  // Приложение под тестами. Раскомментируй, когда в apps/ появится, что гонять:
  // раннер сам поднимет его перед прогоном и погасит после.
  // webServer: {
  //   command: 'pnpm --filter @foxford/<app> dev',
  //   url: 'http://localhost:3000',
  //   reuseExistingServer: !process.env.CI,
  //   timeout: 120_000,
  // },
})
