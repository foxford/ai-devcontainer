# Installed skill invocation: mixed

Вызов: `$foxford-frontend-review` — проверить и исправить frontend-часть задачи от базы f9a1dff08ea198e389e0f48016a409e0f38d7f83, включая коммит `task: card padding`, staged/unstaged, client/new.css и client/reset.css. server/handler.ts — backend payload; user-notes.css и client/legacy.css вне задачи. Прочитана установленная копия SKILL.md и CODESTYLE.md, а не только исходный каталог ADC. Это одна ручная агентная оценка в текущей сессии.

Статус: проверено. Область — client/card.css, client/new.css, client/reset.css; связанная разметка client/page.html. Релевантный HEAD: 7d0d174979008883240721a04ec89e76ec5b2344.

| Место | Правило | Результат |
|---|---|---|
| client/card.css:1 | [FF-CSS-001](../../../delivery/skills/foxford-frontend-review/CODESTYLE.md#ff-css-001) | 15.999px → 16px |
| client/card.css:2 | [FF-CSS-001](../../../delivery/skills/foxford-frontend-review/CODESTYLE.md#ff-css-001) | 7.999px → 8px |
| client/card.css:3, client/page.html:1 | [FF-CSS-002](../../../delivery/skills/foxford-frontend-review/CODESTYLE.md#ff-css-002) | h2 → .cardTitle с назначением класса единственному известному заголовку. Конкурирующих authored-селекторов нет, семантический H2 и красный цвет сохранены |
| client/new.css:1 | [FF-CSS-001](../../../delivery/skills/foxford-frontend-review/CODESTYLE.md#ff-css-001) | 23.999px → 24px |

Исключения: DPR 2 hairline 0.5px, line-height 1.5, width 33.333%, rotate(22.5deg), global reset, scoped CMS rich text. Допустимый margin 8px не отмечен как нарушение. backend .ts пропущен по README и назначению, несмотря на CSS-строки; миграции React/i18n/VM не предлагаются. Legacy 6.777px не оправдывает новые нарушения и сохранён вне задачи.

Проверки: `node --test` — 1/1 pass; Chrome DevTools MCP headless — заголовок H2, текст Title, color rgb(255, 0, 0), padding 16px, margin 8px, один целевой заголовок. Повторная проверка обоих правил: новых нарушений в области не осталось. Защищённые файлы и индекс побайтно совпадают с исходным снимком. Линтер и TypeScript build отсутствуют в этом небольшом проекте, не объявляются выполненными.

Ограничения: один агентный прогон, простой CSS/HTML без React runtime; не доказывает обобщение на все CSS-in-JS и сложный каскад. Специфичность селектора класса отличается численно, итоговый каскад подтверждён в данном fixture. Браузерное измерение подтверждает стили и семантику, не заменяет утверждение макета продукта.
