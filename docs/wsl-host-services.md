# Windows-сервисы из ADC в WSL

В этой схеме Docker Engine работает внутри WSL2 с `networkingMode=mirrored`. WSL достигает Windows localhost, но localhost контейнера относится к самому контейнеру. ADC направляет `host.docker.internal` на Docker host-gateway. Проверка внутри ADC: `getent ahostsv4 host.docker.internal`. Примеры ниже используют подтверждённый адрес Docker-моста `172.17.0.1`; если адрес отличается, замените его в сервисе и Chrome MCP.

## Проверить Windows-сервис

Запустите Obsidian с открытым vault и включённым HTTP в Local REST API. Для Chrome используйте отдельный профиль с CDP в Windows PowerShell:

```powershell
Start-Process "C:\Program Files\Google\Chrome\Application\chrome.exe" -ArgumentList '--remote-debugging-port=9333', "--user-data-dir=$env:LOCALAPPDATA\chrome-cdp", '--no-first-run'
```

В WSL вне контейнера проверьте режим сети и доступность сервисов:

```sh
wslinfo --networking-mode
curl --noproxy '*' --connect-timeout 3 http://127.0.0.1:27123/
curl --noproxy '*' --connect-timeout 3 http://127.0.0.1:9333/json/version
```

Первый HTTP-запрос должен вернуть `status: OK`, второй — JSON с `webSocketDebuggerUrl`. Если WSL не достигает Windows localhost, сначала исправьте запуск приложения или сетевую схему; прокси это не исправит.

## Автозапуск мостов

Установите `socat` в WSL (`sudo apt-get install socat`) и используйте systemd. Образец сервиса Chrome:

```sh
sudo tee /etc/systemd/system/adc-chrome-bridge.service >/dev/null <<'EOF'
[Unit]
Description=ADC bridge to Chrome CDP on Windows
After=network.target docker.service
StartLimitIntervalSec=0

[Service]
User=nobody
ExecStart=/usr/bin/socat TCP4-LISTEN:9334,bind=172.17.0.1,reuseaddr,fork TCP4:127.0.0.1:9333
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now adc-chrome-bridge
systemctl status adc-chrome-bridge --no-pager
```

Для Obsidian создайте отдельный `adc-obsidian-bridge.service` с таким же содержимым, заменив Description на `ADC bridge to Obsidian on Windows` и ExecStart на `/usr/bin/socat TCP4-LISTEN:27125,bind=172.17.0.1,reuseaddr,fork TCP4:127.0.0.1:27123`. Перед запуском службы остановите ручной socat, занимающий тот же порт. Службы перезапускаются при сбое и включаются при старте дистрибутива; Docker-мост должен существовать, а Windows-приложения оставаться запущенными. Службы systemd сами по себе не удерживают WSL-дистрибутив запущенным.

Прокси слушают только адрес Docker-моста. CDP предоставляет агентам управление браузером и его профилем; не привязывайте этот порт к `0.0.0.0` для доступа из LAN.

## Настроить MCP из терминала проекта в ADC

Проверьте транспорт из контейнера:

```sh
curl --noproxy '*' --connect-timeout 3 http://host.docker.internal:27125/
curl --noproxy '*' --connect-timeout 3 http://172.17.0.1:9334/json/version
```

Для Obsidian выполните `adc mcp doctor obsidian --fix` и укажите `http://host.docker.internal:27125/mcp/`, затем API key плагина. Настройка относится к текущему checkout; другие проекты с выбранным Obsidian настройте отдельно.

Для Chrome замените аргументы локального MCP, чтобы он подключался к Windows Chrome, сохранив остальные серверы проекта:

```sh
adc mcp add chrome-devtools --local --json '{"command":"npx","args":["-y","chrome-devtools-mcp@1.7.0","--browserUrl=http://172.17.0.1:9334","--no-performance-crux","--no-usage-statistics"],"x-requires":[]}'
adc mcp sync
adc mcp doctor chrome-devtools
```

Числовой IP в browserUrl позволяет Chrome принять Host заголовок CDP. Вместо флагов запуска отдельного Chromium используются browserUrl и существующий профиль Windows Chrome. Условие на локальный бинарник Chromium сброшено, поскольку браузер уже работает в Windows. `doctor` проверяет MCP initialize/tools/list; соединение с самим Chrome выполняется лениво при первом браузерном инструменте. В новой сессии Codex Chat или CLI в Orca попросите агента вызвать `list_pages` через chrome-devtools: должны появиться вкладки Windows Chrome.

После изменения конфигов закройте прежний агентный чат/терминал и запустите новую сессию. Если Orca сохраняет прежний app-server, переподключите ADC сервер, завершив нужные активные сессии.

Источники: [WSL mirrored networking](https://learn.microsoft.com/en-us/windows/wsl/networking#mirrored-mode-networking), [systemd в WSL](https://learn.microsoft.com/en-us/windows/wsl/systemd), [параметры Chrome DevTools MCP](https://github.com/ChromeDevTools/chrome-devtools-mcp/blob/main/docs/configuration.md), [Local REST API](https://github.com/coddingtonbear/obsidian-local-rest-api).
