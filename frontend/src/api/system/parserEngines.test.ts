import assert from 'node:assert/strict'
import test from 'node:test'

import {
  mergeParserEngineAvailability,
  STATIC_PARSER_ENGINES,
  type ParserEngineInfo,
} from './parserEngineCatalog'

const remote = (overrides: Partial<ParserEngineInfo> & { Name: string }): ParserEngineInfo => ({
  Description: '',
  FileTypes: [],
  ...overrides,
})

test('keeps static file-type topology, refreshes only availability flags', () => {
  const before = STATIC_PARSER_ENGINES.map((e) => ({ ...e }))
  const merged = mergeParserEngineAvailability(STATIC_PARSER_ENGINES, [
    remote({ Name: 'builtin', FileTypes: ['pdf'], Available: false, UnavailableReason: 'offline' }),
  ])
  const builtin = merged.find((e) => e.Name === 'builtin')
  assert.equal(builtin?.Available, false)
  assert.equal(builtin?.UnavailableReason, 'offline')
  assert.deepEqual(builtin?.FileTypes, before.find((e) => e.Name === 'builtin')?.FileTypes)
})

test('appends remote-only engines without shifting the static layout', () => {
  const merged = mergeParserEngineAvailability(STATIC_PARSER_ENGINES, [
    remote({ Name: 'vietnamese_legal', FileTypes: ['pdf'], Available: true }),
  ])
  assert.deepEqual(
    merged.slice(0, STATIC_PARSER_ENGINES.length).map((e) => e.Name),
    STATIC_PARSER_ENGINES.map((e) => e.Name),
  )
  assert.equal(merged.at(-1)?.Name, 'vietnamese_legal')
})

test('null or empty backend response keeps the static catalog', () => {
  assert.deepEqual(mergeParserEngineAvailability(STATIC_PARSER_ENGINES, null), STATIC_PARSER_ENGINES)
  assert.deepEqual(mergeParserEngineAvailability(STATIC_PARSER_ENGINES, []), STATIC_PARSER_ENGINES)
})
