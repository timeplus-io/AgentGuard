import { describe, it, expect } from 'vitest'
import {
  REQUIRED_DEFINITION_FIELDS,
  missingRequiredFields,
  hasRequiredFields,
} from './ruleDefinitionContract.js'

// This suite is the frontend half of the create-form ⇄ backend-validator
// contract. The backend half lives in
// backend/internal/catalog/definition_test.go:
//   - TestDefinitionValidate_RequiredFields  (id/name/version/sql_template required)
//   - TestDefinitionValidate_DescriptionOptional  (empty description still valid)
// If the two halves drift, one side fails — which is exactly the regression
// (form stricter than the contract: description was required by the UI but never
// by validate()) that motivated extracting this contract.

// A complete-but-minimal definition: ONLY the backend-required fields are set;
// every optional field (notably description) is left blank.
const minimal = { id: 'rp-x', name: 'X', version: '1.0.0', sql_template: 'SELECT 1' }

describe('rule definition required-field contract', () => {
  it('required set is exactly id/name/version/sql_template (mirrors backend Definition.validate)', () => {
    expect(REQUIRED_DEFINITION_FIELDS).toEqual(['id', 'name', 'version', 'sql_template'])
  })

  it('saves with a blank description — description is NOT required (regression guard)', () => {
    expect(hasRequiredFields({ ...minimal, description: '' })).toBe(true)
    expect(missingRequiredFields({ ...minimal, description: '' })).toEqual([])
  })

  it('treats other optional metadata (severity/category/fp_rate/hook_sources) as not required', () => {
    expect(hasRequiredFields({ ...minimal, severity: '', category: '   ', fp_rate: '', hook_sources: '' })).toBe(true)
  })

  it('flags each missing required field on create (whitespace counts as missing)', () => {
    expect(missingRequiredFields({ ...minimal, id: '' })).toEqual(['id'])
    expect(missingRequiredFields({ ...minimal, name: '   ' })).toEqual(['name'])
    expect(missingRequiredFields({ ...minimal, version: '' })).toEqual(['version'])
    expect(missingRequiredFields({ ...minimal, sql_template: '\t' })).toEqual(['sql_template'])
    expect(hasRequiredFields({ ...minimal, name: '' })).toBe(false)
  })

  it('does not require id on edit (the existing id is immutable and reused)', () => {
    expect(hasRequiredFields({ ...minimal, id: '' }, { isEdit: true })).toBe(true)
    // ...but other required fields still apply on edit.
    expect(hasRequiredFields({ ...minimal, sql_template: '' }, { isEdit: true })).toBe(false)
  })

  it('tolerates a null/undefined definition without throwing', () => {
    expect(missingRequiredFields(undefined)).toEqual(REQUIRED_DEFINITION_FIELDS)
    expect(hasRequiredFields(null)).toBe(false)
  })
})
