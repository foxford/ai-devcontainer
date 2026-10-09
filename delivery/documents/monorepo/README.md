# Работа с монорепозиторием

## Команды Nx для работы с пакетами

```bash
# Сборка конкретного пакета
nx build @foxford/<имя-пакета>

# Линтинг конкретного пакета
nx lint @foxford/<имя-пакета>

# Проверка типов конкретного пакета
# (у пакетов без TS таргета type-check нет — есть lint:type-check-заглушка)
nx type-check @foxford/<имя-пакета>

# Сборка всех пакетов
nx run-many --target=build

# Линтинг всех пакетов
nx run-many --target=lint

# Сборка только публикуемых пакетов
nx run-many --target=build --projects='tag:publishable'

# Линтинг только публикуемых пакетов
nx run-many --target=lint --projects='tag:publishable'

# Визуализация графа зависимостей
nx graph

# Список всех проектов
nx show projects
```

## Скрипты package.json пакетов

Обязательный набор (машинно проверяется корневым `pnpm.requiredScripts`):

| Скрипт            | Команда                                                       | Описание                                                        |
| ----------------- | ------------------------------------------------------------- | --------------------------------------------------------------- |
| `build`           | `tsup` / `vite build`                                         | Сборка. Для конфиг-пакетов и python-аппов — echo-заглушка       |
| `test`            | `vitest run`                                                  | Тесты (CI добавляет `--passWithNoTests` для пакетов без тестов) |
| `lint:eslint`     | `eslint --cache --cache-strategy content --max-warnings=0 ./` | ESLint с кешированием                                           |
| `lint:type-check` | `tsc --noEmit --pretty`                                       | Типы (для проектов без TS — echo-заглушка)                      |

Опциональные (есть не везде): `test:watch` (vitest watch), `lint:clean`
(`rm -f .eslintcache`), у python-аппов — `py:sync` / `py:lint` / `py:test`.

⚠️ Именно `lint:eslint` / `lint:type-check` / `test` гоняет CI и кеширует Nx;
таргеты `lint` и `type-check` — удобные алиасы поверх них (`lint` дополнительно
чистит кеш ESLint), CI их не использует.

## Публикация пакетов

Инструментарий changesets заведён, но **CI-конвейера публикации нет и публикаций
не было ни разу**: единственный CI — GitLab ([.gitlab-ci.yml](.gitlab-ci.yml)),
он собирает и деплоит только Docker-образы аппов по тегам `<app>-vX.Y.Z`.

Если публикация понадобится — руками из корня:

1. `pnpm changeset` — создать changeset-файл с описанием изменений.
2. `pnpm version-packages` (`changeset version`) — бамп версий + changelog.
3. `pnpm release` (`changeset publish`) — публикация.

Кандидаты — только пакеты с тегом `publishable` и без `private: true`
(сейчас: eslint-config, prettier-config, typescript-config, vitest-config);
реестр публикации не настроен (`publishConfig`/`.npmrc` нет) — настроить до
первого релиза.

## Скрипты из корневого package.json

| Скрипт                                                                                  | Описание                                            |
| --------------------------------------------------------------------------------------- | --------------------------------------------------- |
| `pnpm build`                                                                            | `nx run-many -t build`                              |
| `pnpm test`                                                                             | `nx run-many -t test --passWithNoTests`             |
| `pnpm lint`                                                                             | `nx run-many -t lint:eslint`                        |
| `pnpm type-check`                                                                       | `nx run-many -t lint:type-check`                    |
| `pnpm format`                                                                           | `prettier --write .`                                |
| `pnpm changeset` / `version-packages` / `release`                                       | Changesets (публикаций пока не было, см. выше)      |
| `pnpm clean:cache`                                                                      | Сброс кеша Nx (`nx reset`)                          |

## Фильтрация пакетов по тегу publishable

```bash
# Вывести список публикуемых пакетов
nx show projects --projects='tag:publishable'

# Вывести список НЕ публикуемых пакетов
nx show projects --exclude='tag:publishable'
```

## Скиллы агентов

Скиллы приезжают из платформы, проект держит только свои отличия.
Команды и порядок форка — в [.agents/skills/README.md](.agents/skills/README.md),
список доступного — в `AGENTS.skills.md` (генерируется, в `.gitignore`).
