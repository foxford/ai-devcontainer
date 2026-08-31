import { expect, test } from '@playwright/test'

/**
 * Проверка, что раннер и браузер на месте — она же образец стиля.
 * Удали этот файл, как только появится первый настоящий тест.
 *
 * Пишешь новые — сначала прочитай скилл `playwright-pro`
 * (`.claude/skills/playwright-pro/`): локаторы, фикстуры, разбор flaky.
 */
test.describe('окружение', () => {
  test('браузер поднимается и отдаёт страницу', async ({ page }) => {
    // setContent, а не goto(baseURL): у голого скелетона поднимать ещё нечего,
    // а проверить надо именно связку «раннер + chromium из volume».
    await page.setContent('<h1 data-testid="hello">e2e готов</h1>')

    // getByTestId/getByRole вместо CSS-селекторов: они переживают
    // рефакторинг вёрстки, а `page.locator('.some-class')` — нет.
    await expect(page.getByTestId('hello')).toHaveText('e2e готов')
  })
})
