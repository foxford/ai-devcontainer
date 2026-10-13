# Платформенные скиллы

Каталог содержит общие скиллы ADC. Источник библиотеки задан полем `base.skills` в [каталоге доставки](../catalog.json). В проекте их дополняют форки и собственные скиллы из `.agents/skills/`.

Правьте общий скилл здесь, проектный вариант через `adc skill fork`. Правила платформы для импортированных скиллов находятся в блоке `ai-devcontainer:override:start/end` сразу после метаданных `SKILL.md`. При повторном импорте сохраните этот блок и ограничения из `description`.

## Источники импортированных скиллов

| Скилл | Исходник | Изменения платформы |
|---|---|---|
| [brandkit](brandkit/SKILL.md) | [Leonxlnx/taste-skill/skills/brandkit/SKILL.md](https://github.com/Leonxlnx/taste-skill/blob/HEAD/skills/brandkit/SKILL.md) |  |
| [dependency-auditor](dependency-auditor/SKILL.md) | [alirezarezvani/claude-skills/engineering/skills/dependency-auditor/SKILL.md](https://github.com/alirezarezvani/claude-skills/blob/HEAD/engineering/skills/dependency-auditor/SKILL.md) |  |
| [design-taste-frontend](design-taste-frontend/SKILL.md) | [Leonxlnx/taste-skill/skills/taste-skill/SKILL.md](https://github.com/Leonxlnx/taste-skill/blob/HEAD/skills/taste-skill/SKILL.md) | Рамка платформы и description |
| [humanizer](humanizer/SKILL.md) | [ilyautov/humanizer-ru 3.31.4](https://github.com/ilyautov/humanizer-ru/tree/8c337c3567096758433c475f1d60f2543f6bc94f/skills/humanizer-ru) | Полный skill, сканер и MCP; рамка ADC, жанры, контекстное тире, сохранение смысла. [Происхождение и поправки](humanizer/PROVENANCE.md) |
| [playwright-pro](playwright-pro/SKILL.md) | [alirezarezvani/claude-skills/engineering-team/playwright-pro/skills/pw/SKILL.md](https://github.com/alirezarezvani/claude-skills/blob/HEAD/engineering-team/playwright-pro/skills/pw/SKILL.md) |  |
| [redesign-existing-projects](redesign-existing-projects/SKILL.md) | [Leonxlnx/taste-skill/skills/redesign-skill/SKILL.md](https://github.com/Leonxlnx/taste-skill/blob/HEAD/skills/redesign-skill/SKILL.md) | Рамка платформы и description |
| [senior-architect](senior-architect/SKILL.md) | [alirezarezvani/claude-skills/engineering-team/skills/senior-architect/SKILL.md](https://github.com/alirezarezvani/claude-skills/blob/HEAD/engineering-team/skills/senior-architect/SKILL.md) |  |
| [senior-backend](senior-backend/SKILL.md) | [alirezarezvani/claude-skills/engineering-team/skills/senior-backend/SKILL.md](https://github.com/alirezarezvani/claude-skills/blob/HEAD/engineering-team/skills/senior-backend/SKILL.md) |  |
| [senior-frontend](senior-frontend/SKILL.md) | [alirezarezvani/claude-skills/engineering-team/skills/senior-frontend/SKILL.md](https://github.com/alirezarezvani/claude-skills/blob/HEAD/engineering-team/skills/senior-frontend/SKILL.md) | Рамка платформы и description |
| [senior-fullstack](senior-fullstack/SKILL.md) | [alirezarezvani/claude-skills/engineering-team/skills/senior-fullstack/SKILL.md](https://github.com/alirezarezvani/claude-skills/blob/HEAD/engineering-team/skills/senior-fullstack/SKILL.md) |  |
| [senior-qa](senior-qa/SKILL.md) | [alirezarezvani/claude-skills/engineering-team/skills/senior-qa/SKILL.md](https://github.com/alirezarezvani/claude-skills/blob/HEAD/engineering-team/skills/senior-qa/SKILL.md) |  |
| [senior-secops](senior-secops/SKILL.md) | [alirezarezvani/claude-skills/engineering-team/skills/senior-secops/SKILL.md](https://github.com/alirezarezvani/claude-skills/blob/HEAD/engineering-team/skills/senior-secops/SKILL.md) |  |
| [senior-security](senior-security/SKILL.md) | [alirezarezvani/claude-skills/engineering-team/skills/senior-security/SKILL.md](https://github.com/alirezarezvani/claude-skills/blob/HEAD/engineering-team/skills/senior-security/SKILL.md) |  |
| [stitch-design-taste](stitch-design-taste/SKILL.md) | [Leonxlnx/taste-skill/skills/stitch-skill/SKILL.md](https://github.com/Leonxlnx/taste-skill/blob/HEAD/skills/stitch-skill/SKILL.md) | Рамка платформы и description |
| [ui-ux-pro-max](ui-ux-pro-max/SKILL.md) | [nextlevelbuilder/ui-ux-pro-max-skill/.claude/skills/ui-ux-pro-max/SKILL.md](https://github.com/nextlevelbuilder/ui-ux-pro-max-skill/blob/HEAD/.claude/skills/ui-ux-pro-max/SKILL.md) | Рамка платформы и description; тесты scripts/tests не импортированы |

Данные ниже фиксируют хеши при импорте, а не текущие версии файлов после правок платформы. ADC не использует их при доставке.

<details>
<summary>Хеши импортированных скиллов</summary>

| Скилл | Хеш при импорте |
|---|---|
| `brandkit` | `b63012f3c3d21197e0185d3e9cc7ec40c589fb10e0b5a32a561739de31aa3f20` |
| `dependency-auditor` | `a31ab1d908eb2780729b717f9c9d937aae6afc19d18b6409cae7ba0203582747` |
| `design-taste-frontend` | `6d838b246d0e35d0b53f4f23f98ba7a1dd561937e64f7d0c7553b0928e376c3e` |
| `humanizer` | `9f538eee440359aa11f1cf4b751a0e60662954f57b042112d8fcbcb522b1bb8f` |
| `playwright-pro` | `65857406b8b8b6ce1a5237484d287e9b63d6fad4a0484464b325725cbb8a2423` |
| `redesign-existing-projects` | `b405eee0e0e80fc243f731d9aa368bca307e356db7e6157d27101d369dac6726` |
| `senior-architect` | `0152c1cb90512177153d936557a11bf760d5cbd21470a1305cc727d0a3af1df9` |
| `senior-backend` | `45228f6f893be2bbfcea6be09a7f3fb847635e828a31860a4fd810dbdd26ec0d` |
| `senior-frontend` | `c4845ae10ab5e18b1a6bcfd19067e17545108ee87700f89fd5ea07811ad5096a` |
| `senior-fullstack` | `dd76adf15ea122eecf565b23755950c1c8bb8538362277feace0e12f7e4e0c06` |
| `senior-qa` | `c0ac7af49990f8c82552c3685c3e0fced9747a601ac238625e72761ca81d03df` |
| `senior-secops` | `ae0a60777ec6a1106495eee40e1a814ed55bfdc2767b440ecd5614e42e6c46c9` |
| `senior-security` | `815e7e7540d386358d51515e111a9897fc00bd2d5c907a6e91d1dad482010291` |
| `stitch-design-taste` | `13322f38406cd3abf16ba45e35bdc9b18002d0153a79b4612b97aefc65d5a335` |
| `ui-ux-pro-max` | `ea087c341bfb5b23195c7302027268ede86da802554c18a5c4896a6017b439f9` |

</details>

Graphify и Obsidian поставляются отдельно по выбору проекта: [бандлы](../bundles/). Полный скилл Graphify и сведения об импорте находятся в [бандле Graphify](../bundles/graphify/README.md).
