import path from 'path'

import _resolveModule from 'eslint-module-utils/resolve'
import isGlob from 'is-glob'
import { minimatch } from 'minimatch'

import { isTargetContainsFile } from './utils.js'

// eslint-module-utils is CJS. In ESM interop the default export is the module object,
// while in test mocks (vi.mock) the default export is the function directly.
/** @type {(importPath: string, context: import('eslint').Rule.RuleContext) => string | null} */
const resolve = typeof _resolveModule === 'function' ? _resolveModule : _resolveModule.default

class ImportValidator {
  /** @type {import('eslint').Rule.RuleContext} */
  context

  /** @type {Array<{target: string|string[], from: string, except?: string[], message?: string}>} */
  zones

  /** @type {string} */
  basePath

  /** @type {string} */
  currentFilename

  /** @type {string|undefined} */
  parentPackagePrefix

  /** @type {string} */
  parentPackagePostfix

  /** @type {string[]} */
  exclude

  constructor({
    context,
    zones,
    basePath,
    currentFilename,
    parentPackagePrefix,
    parentPackagePostfix = '',
    exclude = [],
  }) {
    this.context = context
    this.basePath = basePath
    this.currentFilename = currentFilename
    this.parentPackagePrefix = parentPackagePrefix
    this.parentPackagePostfix = parentPackagePostfix
    this.exclude = exclude

    /**
     * Filter zones by target — only keep zones that match the current file
     */
    this.zones = this._filterValidZones({ zones })
  }

  /**
   * Run the validator for the given import path.
   * @param {string} importPath
   */
  dispatch(importPath) {
    /**
     * If current file is excluded, skip validation.
     */
    for (let excludePath of this.exclude) {
      if (minimatch(this.currentFilename, excludePath)) {
        return
      }
    }

    /**
     * Relative imports are always resolved relative to the current file.
     * Package imports go through eslint-module-utils for resolver configuration support.
     */
    const absoluteImportPath = importPath.startsWith('.')
      ? path.resolve(path.dirname(this.currentFilename), importPath)
      : resolve(importPath, this.context)

    if (!absoluteImportPath) {
      // Non-resolvable import (system/node_modules) — skip
      return
    }

    /**
     * If the import is from the same self-feature package, it's always allowed.
     */
    const selfFeatureAbsolutePath = this._getSelfPackagePath()
    if (selfFeatureAbsolutePath && absoluteImportPath?.startsWith(selfFeatureAbsolutePath)) {
      return
    }

    /**
     * Check against each zone.
     */
    this.zones.forEach(({ from, except = [], message }) => {
      const absoluteFrom = path.resolve(this.basePath, from)

      if (!absoluteFrom) {
        return
      }

      if (!absoluteImportPath?.startsWith(absoluteFrom)) {
        return
      }

      for (let exceptPath of except) {
        if (isGlob(exceptPath)) {
          if (minimatch(importPath, exceptPath)) {
            return
          }
        }

        const absoluteExceptPath = path.resolve(absoluteFrom, exceptPath)

        if (importPath === exceptPath || absoluteImportPath?.startsWith(absoluteExceptPath)) {
          return
        }
      }

      throw new Error(`Unexpected path "${importPath}" imported in restricted zone. ${message}`)
    })
  }

  /**
   * Filter zones by checking whether the current file matches the zone's target.
   * @param {{ zones: Array<{target: string|string[]}>}} param
   * @returns {Array}
   * @private
   */
  _filterValidZones({ zones }) {
    return zones.filter((zone) => {
      return []
        .concat(zone.target)
        .map((target) => path.resolve(this.basePath, target))
        .some((targetPath) => {
          return isTargetContainsFile(this.currentFilename, targetPath)
        })
    })
  }

  /**
   * Returns the absolute path of the current file's parent package.
   * @returns {string|null}
   * @private
   */
  _getSelfPackagePath() {
    if (!this.parentPackagePrefix) {
      return null
    }

    const withPlaceholder = /\{name\}/i.test(this.parentPackagePrefix)

    let regex = new RegExp(`/${this.parentPackagePrefix}/(?<packageName>[a-z-_0-9]+)`, 'i')
    if (withPlaceholder) {
      regex = new RegExp(`/${this.parentPackagePrefix.replace('{name}', '(?<packageName>[a-z-_0-9]+)')}`, 'i')
    }

    const matches = this.currentFilename.match(regex)
    if (matches === null) {
      return null
    }

    const packageName = matches.groups?.packageName

    if (!packageName) {
      return null
    }

    if (withPlaceholder) {
      return path.resolve(this.basePath, this.parentPackagePrefix.replace('{name}', packageName))
    }

    return path.resolve(this.basePath, `${this.parentPackagePrefix}/${packageName}`)
  }
}

export { ImportValidator }
