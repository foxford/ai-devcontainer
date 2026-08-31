import { mergeConfig } from 'vitest/config'
import baseConfig from '@foxford/vitest-config'

export default mergeConfig(baseConfig, {
  // здесь можно добавить или переопределить настройки для конкретного пакета
})
