export function canonicalMultiChoiceValue(options, selectedValues) {
  const optionList = Array.isArray(options) ? options : []
  const selected =
    selectedValues instanceof Set ? selectedValues : new Set(Array.isArray(selectedValues) ? selectedValues : [])

  return optionList.filter((opt) => selected.has(opt))
}

export function toggleMultiChoiceValue(options, values, opt) {
  const selected = new Set(Array.isArray(values) ? values : [])

  if (selected.has(opt)) {
    selected.delete(opt)
  } else {
    selected.add(opt)
  }

  return canonicalMultiChoiceValue(options, selected)
}
