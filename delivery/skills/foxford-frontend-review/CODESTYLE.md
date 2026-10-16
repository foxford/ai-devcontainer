# Командные договорённости frontend

Это нормативный документ набора foxford-frontend-review. Правила относятся к frontend-коду в изменениях задачи. Для документированного локального соглашения проекта действует его приоритет; случайный пример legacy-кода такого приоритета не создаёт. Обсуждение и изменение договорённостей проходят обычное командное ревью. ID и явные якоря сохраняются при переименовании заголовков.

- [FF-CSS-001: точность размеров](#ff-css-001)
- [FF-CSS-002: явное оформление компонентов](#ff-css-002)
- [FF-REACT-001: компоненты вместо render helpers](#ff-react-001)
- [FF-REACT-002: один компонент на файл](#ff-react-002)
- [FF-IMPORT-001: публичные границы модулей](#ff-import-001)
- [FF-I18N-001: локализация React-контента](#ff-i18n-001)

<a id="ff-css-001"></a>
## FF-CSS-001 — Целые px для обычных размеров и отступов

**Область:** CSS, CSS Modules, CSS-in-JS и inline styles frontend-компонентов; числовые значения, для которых API подразумевает px, также входят в область. Числа в серверной логике или в тексте страницы не являются CSS-размерами.

**Требование:** обычные размеры и отступы задаются целыми px. Избыточную точность из макета убирают. Намеренные дробные px с понятной причиной сохраняют. Коэффициенты, проценты и углы это правило не округляет. Правило не требует заменять арифметические выражения готовыми значениями.

**Причина:** случайная точность из макетов усложняет чтение и создаёт непреднамеренное выравнивание на доли пикселя. При этом дробные значения нужны для отдельных визуальных эффектов и расчётов.

### Парные примеры

Как не надо — лишняя точность обычного отступа:

```css
.card { padding: 15.999px; }
```

Как надо — намеренный размер:

```css
.card { padding: 16px; }
```

Как не надо — перенесённая из макета ширина кнопки:

```tsx
const Button = styled.button`width: 119.999px;`;
```

Как надо:

```tsx
const Button = styled.button`width: 120px;`;
```

Как не надо — та же избыточная точность в inline style:

```tsx
<div style={{ marginTop: 7.999 }} />
```

Как надо:

```tsx
<div style={{ marginTop: 8 }} />
```

### Исключения и границы

```css
/* Тонкая линия для целевых экранов с DPR 2: намеренная половина CSS px. */
.separator { border-bottom: 0.5px solid currentColor; }
/* Дробный сдвиг компенсирует оптический центр конкретной иконки. */
.icon { transform: translateX(0.5px); }
.text { line-height: 1.5; opacity: 0.75; }
.column { width: 33.333%; transform: rotate(22.5deg); }
.layout { width: calc(100% - 16px); padding: calc(8px * 2); }
```

Динамические вычисления и намеренные дробные значения сохраняют вместе с их причиной из документации, кода или проверяемого контекста. Если назначение дроби или целое целевое значение неоднозначны, предложи уточнение вместо слепого округления всех чисел. Комментарий помогает передать причину, но не обязателен, если она уже подтверждена контекстом.

<a id="ff-css-002"></a>
## FF-CSS-002 — Явное оформление собственных компонентов

**Область:** компонентные frontend-стили, включая вложенные селекторы CSS-in-JS и CSS Modules. Селектор типа, в том числе в цепочке или псевдоклассе, связывает оформление с HTML-тегом. Выбор тега через `styled.button` создаёт собственный компонент и сам по себе не нарушает правило.

**Требование:** собственные элементы оформляют явными классами или styled-компонентами вместо селекторов тегов. Исключения — global/reset и выделенные стили rich text внутри контейнера контента. Приоритетный документ проекта может уточнить область. Сам факт внешнего происхождения разметки исключением не является.

**Причина:** замена семантического тега или добавление вложенного элемента не должна случайно менять оформление соседних элементов. Явный контракт оформления показывает, какой элемент получает стиль.

### Парные примеры

Как не надо:

```tsx
const Card = styled.div`h2 { margin: 0; }`;
<Card><h2>{title}</h2></Card>
```

Как надо:

```tsx
const Card = styled.div``;
const Title = styled.h2`margin: 0;`;
<Card><Title>{title}</Title></Card>
```

Как не надо — CSS и связанная разметка:

```css
.toolbar > button { padding: 8px; }
```

```html
<div class="toolbar"><button>Сохранить</button></div>
```

Как надо — класс описывает назначение:

```css
.toolbar > .saveButton { padding: 8px; }
```

```html
<div class="toolbar"><button class="saveButton">Сохранить</button></div>
```

Как не надо — вложенный селектор CSS Modules:

```css
.card p { margin-top: 8px; }
```

Как надо — явный класс описания, назначенный соответствующему элементу через `styles.description`:

```css
.card .description { margin-top: 8px; }
```

### Исключения и границы

Допустимо глобальное начальное оформление и reset в предназначенных для этого файлах:

```css
/* reset.css — глобальный reset проекта */
html, body { margin: 0; }
button, input { font: inherit; }
```

Допустимы селекторы тегов для rich text в выделенном контейнере контента:

```css
/* .articleBody содержит HTML статьи из редактора контента. */
.articleBody h2 { margin-top: 24px; }
.articleBody p { margin-bottom: 16px; }
```

Название `.content` само по себе не доказывает rich text; проверь происхождение и назначение HTML. Компонент с внешней разметкой, например библиотечный date picker, не становится rich text. Это исключение не разрешает обходить публичный API UI-kit и переопределять его внутренние классы, теги или DOM-структуру.

Автоматическая замена допустима, когда известны все соответствующие элементы и можно подтвердить сохранение каскада и состояний с учётом изменения специфичности. Если разметка недоступна, селектор влияет на несколько неизвестных веток или относится к внутренностям библиотеки, верни предложение с описанием нужного публичного контракта вместо механического добавления класса.

<a id="ff-react-001"></a>
## FF-REACT-001 — Компоненты вместо JSX render helpers

**Область:** React-код production. Render helper — вызываемая как обычная функция подпрограмма, которая собирает JSX для родительского компонента. Имя с `render` само по себе не доказывает нарушение.

**Требование:** такую разметку выделяют в React-компонент с явными props. Компонент объявляют на уровне модуля в отдельном файле по [FF-REACT-002](#ff-react-002). Данные и обработчики передают через props; сохраняют keys, порядок элементов, условия и жизненный цикл.

**Причина:** явная компонентная граница делает зависимости разметки видимыми и позволяет React управлять её жизненным циклом.

### Парные примеры

Как не надо — helper замыкает данные родителя:

```tsx
function Profile({ user, onEdit }) {
  const renderName = () => <button onClick={onEdit}>{user.name}</button>;
  return <section>{renderName()}</section>;
}
```

Как надо — два файла:

```tsx
// ProfileName.tsx
export function ProfileName({ name, onEdit }) {
  return <button onClick={onEdit}>{name}</button>;
}
// Profile.tsx
import { ProfileName } from './ProfileName';
export function Profile({ user, onEdit }) {
  return <section><ProfileName name={user.name} onEdit={onEdit} /></section>;
}
```

Как не надо — helper вызывается для элементов списка:

```tsx
const renderItem = item => <li key={item.id}>{item.name}</li>;
<ul>{items.map(item => renderItem(item))}</ul>
```

Как надо — key остаётся на непосредственном элементе массива:

```tsx
// Item.tsx
export function Item({ item }) { return <li>{item.name}</li>; }
// List.tsx (фрагмент)
import { Item } from './Item';
<ul>{items.map(item => <Item key={item.id} item={item} />)}</ul>
```

### Исключения и границы

Render props — штатный контракт принимающего компонента, а callbacks событий и коллекций допустимы:

```tsx
<DataProvider>{data => <span>{data.name}</span>}</DataProvider>
<VirtualList renderItem={item => <Row item={item} />} />
<button onClick={() => save(id)}>Сохранить</button>
<ul>{items.map(item => <li key={item.id}>{item.name}</li>)}</ul>
const labels = items.map(item => item.name);
```

Проверь контракт получателя callback, а не только возвращаемый JSX. Не превращай render prop в компонент механически. Обычный `.map()` с JSX сам по себе не helper; отдельная функция сборки разметки, вызываемая из него, попадает под правило.

Выделение компонента меняет дерево React. До исправления проследи замыкания, hooks, refs, context, эффекты, мемоизацию и состояние. Новое определение внутри родительского render создаёт новый тип при каждом вызове и может сбрасывать состояние. Hooks в обычном helper, зависимости от порядка вызовов, ref или требование сохранить identity требуют отдельного разбора; если сохранение поведения не подтверждается, оставь предложение. После однозначного выделения проверь обновление props, вызов обработчика с теми же аргументами, перестановку списка с keys и сохранение состояния/cleanup там, где они есть.

<a id="ff-react-002"></a>
## FF-REACT-002 — Один production React-компонент на файл

**Область:** production React-компоненты: функциональные, классовые, обёрнутые в `memo` или `forwardRef`, включая небольшие локальные компоненты.

**Требование:** каждый компонент имеет отдельный файл. Размер и отсутствие экспорта не являются исключениями. Связанные типы, утилиты без компонентной разметки и styled declarations могут оставаться рядом. Обёртка `memo`/`forwardRef` и её реализация образуют один компонент; публичный alias не создаёт второй.

**Причина:** отдельный файл даёт компоненту постоянное место и позволяет читать его зависимости независимо от родителя.

### Парные примеры

Как не надо:

```tsx
// Panel.tsx
const Empty = () => <p>Пусто</p>;
export function Panel({ children }) { return children || <Empty />; }
```

Как надо:

```tsx
// Empty.tsx
export const Empty = () => <p>Пусто</p>;
// Panel.tsx
import { Empty } from './Empty';
export function Panel({ children }) { return children || <Empty />; }
```

Как не надо — memo не делает второй компонент исключением:

```tsx
// Screen.tsx
const Hint = memo(function Hint() { return <small>Подсказка</small>; });
export function Screen() { return <Hint />; }
```

Как надо:

```tsx
// Hint.tsx
export const Hint = memo(function Hint() { return <small>Подсказка</small>; });
// Screen.tsx
import { Hint } from './Hint';
export function Screen() { return <Hint />; }
```

### Исключения и границы

```tsx
// Card.tsx — styled declarations и один production-компонент
const Root = styled.div``;
const Title = styled.h2``;
export function Card({ title }) { return <Root><Title>{title}</Title></Root>; }
// Card.test.tsx — тестовая фикстура
function TestProvider({ children }) { return <Provider>{children}</Provider>; }
// Card.stories.tsx — демонстрационные компоненты
const Demo = () => <Card title="Пример" />;
const Playground = () => <><Demo /><Demo /></>;
```

Исключение определяется назначением компонента, а не одним суффиксом файла: production-компонент, импортируемый приложением из stories или теста, требует отдельного production-файла. Фабрика `styled` — declaration, собственная функция-обёртка с JSX — компонент. При переносе сохрани экспортируемый API, props, refs, зависимости и порядок инициализации; проверь потребителей и тесты. Если перенос требует изменения границ модулей или опасного цикла, предложи конкретный перенос без автоматической перестройки архитектуры.

<a id="ff-import-001"></a>
## FF-IMPORT-001 — Публичный index-контракт между модулями

**Область:** импорты между установленными в проекте модулями frontend-кода. Границу определяют локальные документы, package exports, aliases и существующий публичный entry point, а не каждый каталог. Существование любого index-файла само по себе не доказывает границу модуля.

**Требование:** внешний потребитель использует публичный index-контракт модуля вместо доступа к его реализации. Необходимые прямые внутренние импорты сохраняют. Публичный экспорт добавляют только если это соответствует назначению модуля и не создаёт цикл или ошибку инициализации.

**Причина:** внешний код зависит от поддерживаемого API, поэтому перемещение реализации внутри модуля не затрагивает потребителей.

### Парные примеры

Как не надо — `catalog/index.ts` уже экспортирует ProductCard:

```ts
// checkout/Checkout.tsx
import { ProductCard } from '../catalog/components/ProductCard';
```

Как надо:

```ts
import { ProductCard } from '../catalog';
```

Как не надо — тип также является частью публичного контракта:

```ts
// checkout/model.ts
import type { Product } from '../catalog/model/types';
```

Как надо — `catalog/index.ts` содержит `export type { Product } from './model/types'`:

```ts
import type { Product } from '../catalog';
```

### Исключения и границы

Внутри модуля допустим прямой импорт, в том числе для предотвращения self-barrel цикла:

```ts
// catalog/index.ts
export { ProductCard } from './components/ProductCard';
// catalog/components/ProductCard.tsx
import { selectProduct } from '../model/selectProduct';
```

Явный subpath из `package.json` exports является публичным API; его сохраняют, даже если у пакета есть корневой index:

```ts
import { Button } from '@team/ui/button'; // package exports: ./button
```

Прямой внешний импорт, документированный проектом как необходимый для разрыва цикла/порядка инициализации, сохраняют и отмечают с источником. Если необходимость обнаружена анализом графа без документа, сохрани код и опиши причину как предложение, а не скрывай нарушение за ссылкой на legacy.

До замены проследи реэкспорты, runtime- и type-only зависимости, side effects, циклы и порядок инициализации в обе стороны. Type-only импорт не превращают в runtime. Если публичного экспорта нет или index подтягивает side effects, нельзя автоматически экспортировать все внутренности: предложи минимальный контракт и недостающую проверку. Автоматическая замена допустима только при подтверждённой эквивалентности API и инициализации; широкая перестройка модулей вне области.

<a id="ff-i18n-001"></a>
## FF-I18N-001 — FormattedMessage для локализованного JSX

**Область:** React-проекты с i18n-стеком, который уже поддерживает `FormattedMessage` (например, установленный react-intl соответствующей версии). Способ импорта, descriptors, values и rich text определяют по фактическому API проекта. Правило не требует установки другого стека.

**Требование:** локализованный React-контент выводят через `FormattedMessage`. Для API, которому нужна строка, сохраняют строковый способ локализации.

**Причина:** компонентный API выражает назначение React-контента и поддерживает принятый в проекте вывод сообщений; строковые потребители сохраняют свой контракт.

### Парные примеры

Как не надо:

```tsx
<h1>{intl.formatMessage(messages.title)}</h1>
```

Как надо:

```tsx
<h1><FormattedMessage {...messages.title} /></h1>
```

Как не надо — JSX-контент с параметрами:

```tsx
<p>{intl.formatMessage(messages.greeting, { name })}</p>
```

Как надо:

```tsx
<p><FormattedMessage {...messages.greeting} values={{ name }} /></p>
```

### Исключения и границы

Строковые атрибуты, DOM API, поиск и другие потребители строк сохраняют `formatMessage`:

```tsx
<input placeholder={intl.formatMessage(messages.search)} />
<button aria-label={intl.formatMessage(messages.close)} />
<img alt={intl.formatMessage(messages.cover)} />
useEffect(() => { document.title = intl.formatMessage(messages.title); }, [intl]);
const filtered = rows.filter(row => row.name.includes(intl.formatMessage(messages.query)));
```

Тип prop важнее его имени: `title: string` требует строку, `children: ReactNode` может принимать компонент; проверь контракт. Один результат, используемый и как строка, и как JSX, нельзя целиком заменить на React element. В проекте с другим стеком сохраняют его API, например `{t('title')}` при отсутствии FormattedMessage; новое правило не служит поводом для миграции.

При исправлении сохрани descriptor/id, defaultMessage, values, locale/provider, rich text callbacks, escaping и обработку ошибок. Проверь версию: wrapper/tagName и fallback могут менять DOM и поведение. Если контекст provider, тип prop, версия API или семантика rich text неизвестны, оставь предложение. Удаляй строковый hook/импорт только после проверки всех его использований; проверяй смену локали, интерполяцию и string-only потребителей профильными тестами.
