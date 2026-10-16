# Оценка установленного foxford-frontend-review 1.1.0 — ticket 02

Дата: 2026-10-16. Режим: исправление. Основной агент прочитал SKILL.md и CODESTYLE.md из изолированной установленной копии и применил их к Git-fixture. Это ручная поведенческая оценка инструкций, не независимый запуск модели и не детерминированный анализатор. Полная область и SHA сохранены в [scope.json](scope.json): явная база равна HEAD, релевантных коммитов нет, task-файлы новые. Исходный и конечный код сохранены в [before](before) и [after](after). Все client-файлы — frontend; backend-изменений нет. Отдельные [exceptions](exceptions) — независимые фрагменты для оценки границ правил, не единый компилируемый модуль.

## Замечания и исправления

| Место в before | Правило | Результат |
|---|---|---|
| Dashboard.tsx:9,11 | [FF-REACT-001](../../../../delivery/skills/foxford-frontend-review/CODESTYLE.md#ff-react-001) | renderItem выделен в Item.tsx на уровне модуля; props и handlers переданы явно, key перенесён на непосредственный элемент массива |
| Dashboard.tsx:6–7 | [FF-REACT-002](../../../../delivery/skills/foxford-frontend-review/CODESTYLE.md#ff-react-002) | Empty вынесен в Empty.tsx; компонент Item также получил отдельный файл |
| Dashboard.tsx:4–5 | [FF-IMPORT-001](../../../../delivery/skills/foxford-frontend-review/CODESTYLE.md#ff-import-001) | runtime prefix и type-only Product импортируются из catalog/index; публичные экспорты уже существуют, новых реэкспортов нет |
| Dashboard.tsx:11 | [FF-I18N-001](../../../../delivery/skills/foxford-frontend-review/CODESTYLE.md#ff-i18n-001) | greeting выведен через FormattedMessage; id/defaultMessage/values и provider сохранены; useIntl остался для строкового placeholder |

## Допустимые пары и неоднозначные случаи

| Случай | Решение после чтения установленного правила |
|---|---|
| after/Item.tsx и after/Dashboard.tsx | Компонент вместо helper и один production-компонент на файл; повторных замечаний нет |
| exceptions/cases.tsx: render props DataProvider/VirtualList | Допустимы по контракту принимающего компонента; без исправлений |
| exceptions/cases.tsx: onClick, map с JSX и key | Обычные callbacks допустимы; без исправлений |
| exceptions/cases.tsx: Root/Title styled declarations + Card | Один production-компонент, допустимо |
| exceptions/Card.test.tsx и Card.stories.tsx | TestProvider/Fixture и Demo/Playground — фикстуры/демо, допустимы; приложение их не импортирует |
| catalog/components/Counter.tsx → ../model | Прямой импорт внутри того же catalog; сохранён, self-barrel не создан |
| exceptions/imports.ts → @team/ui/button | Публичный package subpath по MODULES.md, допустим |
| exceptions/consumer.ts → registry/token | Необходимый прямой импорт по MODULES.md: index импортирует consumer; сохранён с указанием основания |
| exceptions/unsafe-consumer.ts → unsafe/value | Только предложение: установить минимальный безопасный публичный контракт; замена на unsafe/index запускает register и бросает ошибку. Код сохранён, перестройка не выполнялась |
| exceptions/cases.tsx: placeholder, aria-label, alt, document.title | String-only API: строковый способ сохранён |
| exceptions/cases.tsx: t('title') | Отдельный проект без FormattedMessage: FF-I18N-001 неприменим, стек не внедрён |
| exceptions/cases.tsx: Stateful/renderBody с useState | FF-REACT-001: только предложение выделить компонент после согласования владения состоянием и lifecycle; автоматическая замена не выполнена |

В exceptions/cases.tsx фрагменты оцениваются независимо, поэтому Card и Stateful не считаются двумя компонентами одного production-файла. Для опасного barrel и helper с hooks остаются предложения. Замечаний в остальных парных исключениях нет. Файлы первоначальных исключений и индекс Git-fixture сохранены побайтно; unsafe-сценарий добавлен и оценён отдельно без записи в него. CSS-нарушений в задаче нет; VM/UI-kit правила не добавлялись.

## Проверки

Установка: `npx --yes skills add <adc-checkout>/delivery/skills/foxford-frontend-review --agent claude-code codex --copy --yes`, exit 0. Все четыре файла в .agents/skills и .claude/skills сверены с поставкой, относительные ссылки разрешаются, версия 1.1.0. `npx --yes skills list --json` подтверждает обоих агентов. Локальный путь нужен только при установке; поставка и примеры не зависят от истории сессии или абсолютного пути. GitHub-публикация не проверялась.

Для воспроизведения runtime используйте существующий набор React 17, react-dom 17, react-intl, jsdom и TypeScript: `NODE_PATH=<existing-node-modules> node --test docs/validation/foxford-frontend-review/ticket02/behavior.test.cjs`. Зависимости не устанавливались; использован уже имеющийся frontend toolchain соседнего проекта. Два теста before/after прошли. Проверены JSX-текст и отсутствие дополнительной обёртки, интерполяция Ada, переключение en/fr, placeholder, onSelect с исходным id, обновление имени, перестановка keys, сохранение состояния счётчика и identity DOM input, cleanup при удалении строк и пустое состояние. Это jsdom component test, без браузерной автоматизации; реальный ref callback и все возможные React runtime не покрыты.

TypeScript: копии before/after рядом с существующим node_modules, `tsc --noEmit --jsx react --esModuleInterop --skipLibCheck --types react --lib es2020,dom before/Dashboard.tsx after/Dashboard.tsx`, exit 0. Runtime тест транспилирует TypeScript, поэтому отдельный tsc нужен для проверки типов. Фрагменты exceptions не выдаются за typechecked приложение. Публичный catalog не содержит side effects и обратных импортов из Dashboard; Counter оставил внутренний импорт. Тест импортирует оба варианта и подтверждает доступность prefix/Counter при инициализации.

Метаданные, уникальность шести ID/якорей, несколько пар примеров каждого правила, относительные ссылки и отсутствие абсолютных путей в поставке проверены отдельно. Полные обязательные гейты ADC предприняты: `gofmt -w bin/adc`, `go vet ./...`, `go test -race ./...`, `go build -o .build/adc ./bin/adc` — каждый недоступен (command not found). Go и runtime не менялись. Тикет остаётся открытым до прохождения гейтов; успешные два component test не заменяют полный набор ADC. Оценка других версий i18n, rich text, классов, memo/forwardRef и реального браузера ограничена чтением нормативных примеров, runtime успех этих случаев не заявляется.

## Независимое review

Code-review по зафиксированной базе dd10a950acd92fe38718d12449bcda5e56fc9a77 и всему рабочему результату: Standards — 0 замечаний, Spec — 0 блокирующих замечаний. Оба проверяющих отдельно подтвердили ограничение Go-гейтов. Ручная вычитка humanizer для технической документации пройдена; сканер и числовой балл не использовались.
