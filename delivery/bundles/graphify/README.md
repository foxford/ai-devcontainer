# Graphify

Бандл подключается capability `graphify`. [bundle.json](bundle.json) декларирует всю поставку: CLI, полный скилл, инструкции и MCP. Пути exports отсчитываются от этого каталога.

Upstream: [Graphify-Labs/graphify](https://github.com/Graphify-Labs/graphify), пакет [graphifyy 0.9.82](https://pypi.org/project/graphifyy/0.9.82/). `graphifyy` — имя Python-пакета, `graphify` — установленная команда. Скилл и references импортированы из wheel той же версии: graphify/skill-codex.md и graphify/skills/codex/references/.

Полное тело upstream и references сохранены. Локальные изменения: description и блок ai-devcontainer:override:start/end после frontmatter. Блок связывает скилл с автоматическим индексом и MCP ADC, отключает ручную установку и agent hooks, сохраняет полный анализ материалов по запросу. При переимпорте верните рамку и обновите её вместе с exports.tools, .graphify_version и проверками.

SHA-256 исходного skill-codex.md до рамки: `ab3be669f19a95232afecacb46dcb42e2354a1da229f3aaae92aeaec8bf3ec28`.

Автоиндекс сохраняется отдельно в каждом checkout, без модельных запросов. MCP ADC обеспечивает query, explain, path и affected через публичный CLI. Рецепты экспорта, семантического извлечения и медиа находятся в [полном скилле](graphify/SKILL.md) и его references.
