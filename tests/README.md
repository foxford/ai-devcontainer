# Проверки ADC

TestDockerLegacyMigration выполняет полный backup/import двух старых devcontainer на отдельном Docker fixture: снимки маунтов и DinD, inactive profiles и projects, writable layer OpenCode, проверка контрольных сумм, выбор общего аккаунта, конфликты сессий, ссылка Hermes и сохранение после перезапуска. Тест использует фиктивные данные и не меняет рабочие контейнеры разработчика.

Все проверки платформы запускаются через Go. Unit-тесты лежат рядом с кодом в `bin/adc/`, Docker smoke в `bin/adc/integration/`.

| Команда | Что проверяет |
|---|---|
| `gofmt -l bin/adc` | Форматирование; вывод должен быть пустым |
| `go vet ./...` | Статические проверки Go |
| `go test -race ./...` | CLI, реестр и блокировки, установщик, каталог, доставка документов, skills и MCP, секреты и настройка Orca |
| `go build -o .build/adc ./bin/adc` | Сборка CLI |
| `ADC_TOOL_BUNDLES_INTEGRATION=1 ADC_INTEGRATION=1 go test -v -count=1 -timeout=35m ./bin/adc/integration` | Настоящий Docker runtime: два репозитория, Orca worktree, терминал, адаптер агента, оболочки, баннер, Graphify CLI/индекс/MCP, OmniRoute dashboard и данные после пересоздания |

Для отдельного набора используй `go test ./bin/adc/runtime -run TestMCP`. Фикстуры изолированы от аккаунтов и домашнего каталога разработчика. Docker smoke запускает настоящую Orca и подменяет только CLI агента: запросов к модели и авторизации нет. Без `ADC_INTEGRATION=1` Docker smoke пропускается, поэтому обычный `go test ./...` не требует Docker.

CI запускает Go-проверки на Linux, macOS и Windows и собирает бинарник для amd64/arm64 каждой ОС. Docker smoke с реальными тулзами выполняется на Linux; он скачивает закреплённые Graphify и OmniRoute, не использует аккаунты и не вызывает модели. Для быстрого smoke без их установки опустите ADC_TOOL_BUNDLES_INTEGRATION. Установка системных пакетов и desktop pairing на macOS/Windows требуют проверки на этих ОС.
