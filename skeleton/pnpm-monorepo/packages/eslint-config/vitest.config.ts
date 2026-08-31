/**
 * Entry package: vitest-config"
 */
import { mergeConfig } from 'vitest/config'
import baseConfig from '@foxford/vitest-config'

export default mergeConfig(baseConfig, {
  cacheDir: './node_modules/.cache/vite',
  test: {
    env: {
      NODE_ENV: 'test',
    },
    environment: 'node',
    exclude: [
      '**/node_modules/**',
      '**/dist/**',
      '**/build/**',
      '**/cypress/**',
      '**/.{idea,git,cache,output,temp}/**',
      '**/{karma,rollup,webpack,vite,vitest,jest,ava,babel,nyc,cypress,tsup,build,eslint,prettier}.config.*',
    ],
    globals: false,
    include: ['**/*.{test,spec}.?(c|m)[jt]s?(x)'],
  },
})
