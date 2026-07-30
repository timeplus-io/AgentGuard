// packageScope.js — pure helpers for scoping the rules view to a selected rule
// package on the /rules page. A null selection means "All packages" (no
// filtering); a package narrows both the installed-rules grid and the
// available-to-install grid to that package's member rule ids.

// packageMemberSet returns a Set of the package's member rule ids, or null when
// no package is selected (the "All" view). A package with no rules array yields
// an empty set rather than throwing.
export function packageMemberSet(pkg) {
  if (!pkg) return null
  return new Set(pkg.rules || [])
}

// scopeToPackage filters items to those whose id is in memberSet, preserving
// input order. A null memberSet returns the original array unchanged (same
// reference) so the "All" path stays allocation-free. getId defaults to the
// common `.id` shape; pass a custom accessor for other row shapes.
export function scopeToPackage(items, memberSet, getId = x => x.id) {
  if (!memberSet) return items
  return items.filter(it => memberSet.has(getId(it)))
}
