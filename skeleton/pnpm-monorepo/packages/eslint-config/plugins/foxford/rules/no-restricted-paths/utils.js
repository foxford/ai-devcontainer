import path from 'path'

import isGlob from 'is-glob'
import { minimatch } from 'minimatch'

/**
 * Checks whether filepath is contained in targetPath (i.e. filepath is a child
 * of target). Supports glob patterns in targetPath.
 *
 * @param {string} filepath
 * @param {string} target
 * @returns {boolean}
 */
const isTargetContainsFile = (filepath, target) => {
  if (isGlob(target)) {
    return minimatch(filepath, target)
  }

  const relative = path.relative(target, filepath)
  return relative === '' || !relative.startsWith('..')
}

export { isTargetContainsFile }
