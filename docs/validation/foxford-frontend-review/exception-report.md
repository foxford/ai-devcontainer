# Installed skill invocation: exception

Вызов: `$foxford-frontend-review` — проверить и исправить frontend-часть задачи от базы 2f6266cbe2d2d17c6bd0a745b9f0c696937ebfaf, включая коммит `task: card padding`, staged/unstaged, client/new.css и client/reset.css. server/handler.ts — backend payload; user-notes.css и client/legacy.css вне задачи. Прочитана установленная копия SKILL.md и CODESTYLE.md, а не только исходный каталог ADC. Это одна ручная агентная оценка в текущей сессии.

Статус: проверено. Область — client/card.css, client/new.css, client/reset.css; связанная разметка client/page.html. Релевантный HEAD: 59aa1f2fe3ede4bcb5349b63b027e26539a052f1.

| Место | Правило | Результат |
|---|---|---|
| client/card.css:1 | [FF-CSS-001](../../../delivery/skills/foxford-frontend-review/CODESTYLE.md#ff-css-001) | Сохранено 15.999px: приоритетный документ AGENTS.md, исключение только для card.css |
| client/card.css:2 | [FF-CSS-001](../../../delivery/skills/foxford-frontend-review/CODESTYLE.md#ff-css-001) | Сохранено 7.999px по тому же исключению |
| client/card.css:3, client/page.html:1 | [FF-CSS-002](../../../delivery/skills/foxford-frontend-review/CODESTYLE.md#ff-css-002) | h2 → .cardTitle с назначением класса единственному известному заголовку. Конкурирующих authored-селекторов нет, семантический H2 и красный цвет сохранены |
| client/new.css:1 | [FF-CSS-001](../../../delivery/skills/foxford-frontend-review/CODESTYLE.md#ff-css-001) | 23.999px → 24px |

Исключения: DPR 2 hairline 0.5px, line-height 1.5, width 33.333%, rotate(22.5deg), global reset, scoped CMS rich text. Допустимый margin 8px не отмечен как нарушение. backend .ts пропущен по README и назначению, несмотря на CSS-строки; миграции React/i18n/VM не предлагаются. Legacy 6.777px не оправдывает новые нарушения и сохранён вне задачи.

Проверки: `node --test` — 1/1 pass; Chrome DevTools MCP headless — заголовок H2, текст Title, color rgb(255, 0, 0), padding 15.999px, margin 7.999px, один целевой заголовок. Повторная проверка обоих правил: новых нарушений в области не осталось. Защищённые файлы и индекс побайтно совпадают с исходным снимком. Линтер и TypeScript build отсутствуют в этом небольшом проекте, не объявляются выполненными.

Ограничения: один агентный прогон, простой CSS/HTML без React runtime; не доказывает обобщение на все CSS-in-JS и сложный каскад. Специфичность селектора класса отличается численно, итоговый каскад подтверждён в данном fixture. Браузерное измерение подтверждает стили и семантику, не заменяет утверждение макета продукта.
