import { execSync } from 'child_process'
import * as path from 'path'

import {
  addProjectConfiguration,
  formatFiles,
  generateFiles,
  runTasksInSerial,
  addDependenciesToPackageJson,
  updateJson,
} from '@nx/devkit'

import type { PackageGeneratorSchema } from './schema'
import type { Tree } from '@nx/devkit'

/**
 * Функция для установки пакетов
 */
function installPackages() {
  console.log('Устанавливаем пакеты...')
  execSync('pnpm install', { stdio: 'inherit' })
}

export async function packageGenerator(tree: Tree, options: PackageGeneratorSchema) {
  const projectRoot = `packages/${options.name}`
  addProjectConfiguration(tree, `@foxford/${options.name}`, {
    projectType: 'library',
    root: projectRoot,
    sourceRoot: `${projectRoot}/src`,
    targets: {},
  })

  generateFiles(tree, path.join(import.meta.dirname, 'files'), projectRoot, { ...options, projectRoot })

  // Если выбрана опция useTsup, генерируем tsup.config.ts
  if (options.useTsup) {
    generateFiles(tree, path.join(import.meta.dirname, 'tsup'), projectRoot, { ...options, projectRoot })
    addDependenciesToPackageJson(tree, {}, { tsup: 'catalog:dev' }, `${projectRoot}/package.json`)
  }

  // Явно устанавливаем sourceRoot после всех generateFiles,
  // так как шаблоны используют Nx-токен {projectRoot} вместо реального пути
  updateJson(tree, `${projectRoot}/project.json`, (json) => {
    json.sourceRoot = `${projectRoot}/src`
    return json
  })

  updateJson(tree, `${projectRoot}/package.json`, (json) => {
    json.scripts = json.scripts || {}

    if (options.useTsup) {
      json.scripts.build = 'tsup'
    }

    return json
  })

  await formatFiles(tree)

  return () => {
    installPackages()
  }
}

export default packageGenerator

export const packageGeneratorSchematic = async (tree: Tree, options: PackageGeneratorSchema) => {
  const task = await packageGenerator(tree, options)
  return runTasksInSerial(task)
}
