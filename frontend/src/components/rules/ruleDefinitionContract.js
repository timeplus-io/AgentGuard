// REQUIRED_DEFINITION_FIELDS mirrors the backend's Definition.validate() required
// set (backend/internal/catalog/definition.go): a rule definition is valid only
// when it carries id, name, version, and sql_template. Everything else —
// description, severity, category, fp_rate, hook_sources, config, udfs — is
// OPTIONAL, because the backend validator does not require it.
//
// Keeping this list in lockstep with the Go validator is what stops the create
// form from being STRICTER than the contract. A #33 Phase B regression made the
// form require a non-empty description even though validate() never has; the
// create form's Save gate now derives its required set from here, and
// ruleDefinitionContract.test.js (frontend) + TestDefinitionValidate_RequiredFields
// / TestDefinitionValidate_DescriptionOptional (backend) pin both halves so the
// two cannot silently drift again.
//
// `id` is enforced only on create; edit reuses the existing (immutable) id.
export const REQUIRED_DEFINITION_FIELDS = ['id', 'name', 'version', 'sql_template']

// missingRequiredFields returns the subset of REQUIRED_DEFINITION_FIELDS that are
// absent (empty or whitespace-only) on def. On edit, `id` is never reported
// missing. The return is an array so callers can both gate (length === 0) and,
// if desired, surface which fields are missing.
export function missingRequiredFields(def, { isEdit = false } = {}) {
  const d = def || {}
  return REQUIRED_DEFINITION_FIELDS.filter((field) => {
    if (field === 'id' && isEdit) return false
    return String(d[field] ?? '').trim() === ''
  })
}

// hasRequiredFields is the boolean Save gate: true when no required field is
// missing. It deliberately does NOT consider description or any other optional
// field — matching the backend contract.
export function hasRequiredFields(def, opts) {
  return missingRequiredFields(def, opts).length === 0
}
