## pnpm и Nx — только для этого проекта

Используй pnpm, закреплённый полем packageManager в package.json. Версию Node бери из .tool-versions/engines проекта. Не меняй стек ради выбранного скилла. Прочитай MONOREPO.md, если он есть. Перед завершением выполни существующие скрипты lint, type-check, test и build из корня, либо соответствующие Nx targets затронутых пакетов. Не добавляй --passWithNoTests к run-many.
