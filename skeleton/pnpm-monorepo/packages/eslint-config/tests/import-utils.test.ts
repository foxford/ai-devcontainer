import { describe, expect, it } from 'vitest'

import { getImportText, getImportTypeText, getSpecifierName } from '../plugins/foxford/utils/import-utils.js'

type GetImportTextArgs = { source: string; values?: string; defaultValue?: string }
const getImportTextFn = getImportText as (args: GetImportTextArgs) => string

describe('getImportTypeText', () => {
  it('generates type import string', () => {
    expect(getImportTypeText({ source: "'./foo'", types: 'Foo' })).toBe("import type { Foo } from './foo'")
  })

  it('generates type import with multiple types', () => {
    expect(getImportTypeText({ source: "'./bar'", types: 'Foo, Bar' })).toBe("import type { Foo, Bar } from './bar'")
  })

  it('lineFeedEnded adds newline after type import', () => {
    const result = getImportTypeText.lineFeedEnded.call(getImportTypeText, { source: "'./foo'", types: 'Foo' })
    expect(result).toBe("import type { Foo } from './foo'\n")
  })

  it('lineFeedStarted adds newline before type import', () => {
    const result = getImportTypeText.lineFeedStarted.call(getImportTypeText, { source: "'./foo'", types: 'Foo' })
    expect(result).toBe("\nimport type { Foo } from './foo'")
  })

  it('lineFeedEnded throws if this is not a function', () => {
    expect(() => {
      getImportTypeText.lineFeedEnded.call('not-a-function', { source: "'./foo'", types: 'Foo' })
    }).toThrow(TypeError)
  })

  it('lineFeedStarted throws if this is not a function', () => {
    expect(() => {
      getImportTypeText.lineFeedStarted.call(null, { source: "'./foo'", types: 'Foo' })
    }).toThrow(TypeError)
  })
})

describe('getImportText', () => {
  it('generates import with named values', () => {
    expect(getImportTextFn({ source: "'./foo'", values: 'foo' })).toBe("import { foo } from './foo'")
  })

  it('generates import with default value only', () => {
    expect(getImportTextFn({ defaultValue: 'Foo', source: "'./foo'" })).toBe("import Foo from './foo'")
  })

  it('generates import with both default and named values', () => {
    expect(getImportText({ defaultValue: 'Foo', source: "'./foo'", values: 'bar' })).toBe(
      "import Foo, { bar } from './foo'"
    )
  })

  it('lineFeedEnded adds newline after import', () => {
    const result = getImportText.lineFeedEnded.call(getImportText, { source: "'./foo'", values: 'foo' })
    expect(result).toBe("import { foo } from './foo'\n")
  })

  it('lineFeedStarted adds newline before import', () => {
    const result = getImportText.lineFeedStarted.call(getImportText, { source: "'./foo'", values: 'foo' })
    expect(result).toBe("\nimport { foo } from './foo'")
  })

  it('lineFeedEnded throws if this is not a function', () => {
    expect(() => {
      getImportText.lineFeedEnded.call(null, { source: "'./foo'", values: 'foo' })
    }).toThrow(TypeError)
  })

  it('lineFeedStarted throws if this is not a function', () => {
    expect(() => {
      getImportText.lineFeedStarted.call(42, { source: "'./foo'", values: 'foo' })
    }).toThrow(TypeError)
  })
})

describe('getSpecifierName', () => {
  it('returns just the name when imported and local names are the same', () => {
    const specifier = { imported: { name: 'foo' }, local: { name: 'foo' } }
    expect(getSpecifierName(specifier)).toBe('foo')
  })

  it('returns aliased form when names differ', () => {
    const specifier = { imported: { name: 'foo' }, local: { name: 'bar' } }
    expect(getSpecifierName(specifier)).toBe('foo as bar')
  })
})
