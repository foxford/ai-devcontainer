// Весь стек линтинга живёт в @foxford/eslint-config (flat-config, eslint 9):
// слои js/ts/imports/testing/prettier + кастомные правила foxford/*.
// Для react-кода используйте пресет пакета: '@foxford/eslint-config/react',
// для node-сервисов — '@foxford/eslint-config/node'.
import config from '@foxford/eslint-config';

export default config;
