# Проверки ADC

TestDockerLegacyMigration выполняет полный backup/import двух старых devcontainer на отдельном Docker fixture: снимки маунтов и DinD, inactive profiles и projects, writable layer OpenCode, проверка контрольных сумм, выбор общего аккаунта, конфликты сессий, ссылка Hermes и сохранение после перезапуска. Тест использует фиктивные данные и не меняет рабочие контейнеры разработчика.

Все проверки платформы запускаются через Go. Unit-тесты лежат рядом с кодом в `bin/adc/`, Docker smoke в `bin/adc/integration/`.

TestPOSIXBootstrapReplacesLegacyAtDefaultPaths проверяет переход старого клона на main, копию платформы, замену прежнего симлинка ADC и доставку PATH/completion в изолированный HOME. Компилятор подменяется готовым настоящим Go-бинарником; системные пакеты и Orca desktop не устанавливаются. Go-тесты отдельно проверяют блокировку активных контейнеров, грязного клона и разошедшейся main. TestDockerRuntimeRemapsIndependentVolumeOwners запускает настоящий runtime с UID/GID 12345 и проверяет запись в home/tools и сохранение владельца внешней цели симлинка. HTTP-тесты проверяют отдачу большого графа через симлинк в пути каталога, включая macOS /var. .gitattributes закрепляет LF на всех ОС.

| Команда | Что проверяет |
|---|---|
| `gofmt -l bin/adc` | Форматирование; вывод должен быть пустым |
| `go vet ./...` | Статические проверки Go |
| `go test -race ./...` | CLI, реестр и блокировки, установщик, каталог, доставка документов, skills и MCP, секреты и настройка Orca |
| `go build -o .build/adc ./bin/adc` | Сборка CLI |
| `ADC_TOOL_BUNDLES_INTEGRATION=1 ADC_INTEGRATION=1 go test -v -count=1 -timeout=35m ./bin/adc/integration` | Настоящий Docker runtime: два репозитория, Orca worktree, терминал, адаптер агента, оболочки, баннер, Graphify CLI/индекс/MCP, OmniRoute dashboard и данные после пересоздания |

Для отдельного набора используй `go test ./bin/adc/runtime -run TestMCP`. Фикстуры изолированы от аккаунтов и домашнего каталога разработчика. Docker smoke запускает настоящую Orca и подменяет только CLI агента: запросов к модели и авторизации нет. Без `ADC_INTEGRATION=1` Docker smoke пропускается, поэтому обычный `go test ./...` не требует Docker.

Go-тесты workspace replacement проверяют сохранение прежнего контейнера до ready, ошибки stop/rename/run/cleanup, восстановление исходного состояния после ошибки создания, прерывания между операциями, повтор после Ctrl+C и отказ менять контейнеры с чужими идентификаторами. Docker smoke дополнен отказом запуска с отсутствующим image ID и потерей ответа после настоящего docker rename: Go-прокси передаёт команды реальному Docker, а ADC восстанавливает контейнер или продолжает переключение по журналу. Реальный запуск этих новых сценариев в окружении без Docker не подтверждён.

CI запускает Go-проверки на Linux, macOS и Windows и собирает бинарник для amd64/arm64 каждой ОС. Docker smoke с реальными тулзами выполняется на Linux; он скачивает закреплённые Graphify и OmniRoute, не использует аккаунты и не вызывает модели. Для быстрого smoke без их установки опустите ADC_TOOL_BUNDLES_INTEGRATION. Установка системных пакетов и desktop pairing на macOS/Windows требуют проверки на этих ОС.

Docker smoke также проверяет доступность user namespace с профилем ADC. В режиме ADC_TOOL_BUNDLES_INTEGRATION устанавливается native Codex 0.162.1 во временный prefix; встроенный профиль :workspace должен читать документы ADC, разрешать запись в проект и запрещать запись в файл вне проекта, несмотря на его Unix права. Пользовательский CODEX_HOME и авторизации не используются; запросов к модели нет.
