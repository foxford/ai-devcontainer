# Установленный foxford-frontend-review 1.3.0 — ticket 04

Дата: 2026-10-16. Основной агент вызвал установленную через npx skills копию в режиме исправления, прочитал SKILL.md и CODESTYLE.md, проверил область Git и применил правила к fixture. Это ручная оценка инструкций основной сессией, без независимого запуска модели; она не измеряет надёжность всех агентов. База и HEAD совпадают, релевантных коммитов нет; примеры — новые рабочие файлы. SHA и контракт действий: [scope.json](scope.json); исходный Git-вывод: [git-before.json](git-before.json). notes.txt — пользовательская работа вне задачи. Backend отсутствует. model.cjs принадлежит frontend-сценарию, что подтверждается потребителями и контрактом fixture.

## API и установка

Использован реальный @foxford/vm 2.0.2 из существующего toolchain stoege с React 17, Effector и TypeScript; зависимости проекта не устанавливались. Версия, ревизия и SHA-256 файлов пакета: [api-evidence.json](api-evidence.json). Ссылки на зафиксированную ревизию находятся в [CODESTYLE.md](../../../../delivery/skills/foxford-frontend-review/CODESTYLE.md#ff-vm-001). Личная история не нужна для применения правил.

Изолированная установка через npx skills для Claude Code и Codex прошла, четыре файла каждой копии побайтно совпали с поставкой 1.3.0. Команда и вывод skills list: [installation.txt](installation.txt). Локальные ссылки и уникальность якорей проверены. Отчёт не является runtime-зависимостью установленного skill.

## Фактический результат вызова

| Место | Правило | Результат |
|---|---|---|
| before/client/Pure.tsx:4 | FF-VM-001 | Повторный inline mapper вынесен в чистую функцию double |
| before/client/Total.tsx:4 | [FF-VM-001](../../../../delivery/skills/foxford-frontend-review/CODESTYLE.md#ff-vm-001) | Пустые зависимости заменены на [factor]; при прежнем factor ссылка сохраняется, при смене используется актуальный mapper |
| before/client/Text.tsx:4 | [FF-VM-002](../../../../delivery/skills/foxford-frontend-review/CODESTYLE.md#ff-vm-002) | Подписка только ради текста заменена signal |
| before/client/Controls.tsx:4 | FF-VM-002 | signal в условии/disabled заменён безусловными use до return |
| before/model.cjs:4 | [FF-VM-003](../../../../delivery/skills/foxford-frontend-review/CODESTYLE.md#ff-vm-003) | setDoubled пишет next/2 в исходный count по явному контракту, вместо записи в проекцию |
| before/model.cjs:5 | [FF-VM-004](../../../../delivery/skills/foxford-frontend-review/CODESTYLE.md#ff-vm-004) | Частичная очистка state заменена vm.destroy у владельца завершённого сценария |
| before/client/Owner.tsx:4 | FF-VM-004 | Hook на верхнем уровне сохранён; click разрешает последующую очистку без немедленного rerender |
| cases/valid/Once.ts:4–5 | FF-VM-001 | Однократное поле VM с inline mapper и Array.map допустимы |
| cases/valid/Combined.tsx:4–5 | FF-VM-002 | use нужен для disabled и обработчика; тот же count допустимо вывести в JSX |
| cases/ambiguous/Write.ts:5 | FF-VM-003 | Только предложение команды владельца: clamp необратим, исходный count неизвестен |
| cases/ambiguous/Child.tsx:3–5 | FF-VM-004 | Потребитель общей VM не получает destroy/enableAutoDestroy; код сохранён |
| cases/no-vm/Counter.tsx:1–6 | FF-VM-001–004 | VM-правила неприменимы; useState/useEffect сохранены, миграция и запрет логики в hooks не предложены |
| cases/unknown/Unknown.tsx:2–3 | FF-VM-001–004 | Неполная VM-проверка: версия 99.0.0 и проектный контракт не подтверждены; догадочных исправлений нет |

Основной результат исправляемого fixture: проверено, повторная проверка FF-VM-001–004 не нашла оставшихся нарушений. Отдельный ambiguous fixture оставляет одно нарушение readonly и предложение без правки; unknown fixture остаётся неполным. Другие правила не требуют правок: CSS/UI-kit отсутствуют, локализация не установлена, по одному компоненту на файл, импорты публичные. До/после сохранены в before/after. Индекс и пользовательский notes.txt сохранены: [protection.json](protection.json). Подготовленные cases оценены без изменений, хеши: [case-protection.json](case-protection.json). Область оценки ограничена VM; staged/committed scope и read-only на полном Git-проекте проверялись ранее в ticket 01 и здесь не переоценивались.

## Проверки и ограничения

Из корня ADC: `NODE_PATH=<existing-compatible-node_modules> node --test docs/validation/foxford-frontend-review/ticket04/behavior.test.cjs`. Восемь поведенческих проверок прошли: [behavior.txt](behavior.txt). Проверены обновления источника, props/factor и повторный render; текст, условные ветки и disabled; запись и уведомление подписчика; onDestroy ровно один раз и недоступность состояния после destroy; unmount с разрешением и без, disableAutoDestroy. Отдельный тест подтверждает cleanup при rerender после разрешения в 2.0.2. Негативные примеры воспроизводят исходные ошибки. Это jsdom-проверки поведения примеров, без браузера; production UI не изменялся.

Команда `NODE_PATH=<existing-compatible-node_modules> node docs/validation/foxford-frontend-review/ticket04/typecheck.cjs` проверяет реальные декларации пакета: before содержит ошибку типа disabled, after — 0 диагностик; readonly set отклоняется; журнал typecheck.txt. В after нет any/as для обхода контракта. Повторите команды с уже доступными @foxford/vm 2.0.2, React/ReactDOM 17, TypeScript, @types/react и jsdom. Эти зависимости не входят в поставку skill; harness оценивает примеры, а не runtime платформы ADC.

`git diff --check` прошёл. Обязательные команды ADC запускались: gofmt -w bin/adc, go vet ./..., go test -race ./..., go build -o .build/adc ./bin/adc. Все недоступны (exit 127: go/gofmt не найдены). Полная проверка платформы не выполнена; тикет остаётся открытым. StrictMode/remount и другие версии VM не оценивались. Docker/runtime не менялись.

Code-review от фиксированной базы 1af32b94da687510c159f8bdbe06465a4958fecd, включая рабочие и новые файлы: независимые Standards и Spec дали по 0 оставшихся замечаний. Пропущенная запятая в USAGE.md исправлена. Полный доступный набор оценки ticket 02 + ticket 04 прошёл: 10/10, журнал full-evaluation.txt; Go-suite по-прежнему недоступен. Русский черновик вычитан по humanizer в режиме своего черновика, без сканера и численного балла.
