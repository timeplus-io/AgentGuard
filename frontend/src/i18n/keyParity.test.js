import { describe, it, expect } from 'vitest'

const en = import.meta.glob('./locales/en/*.json', { eager: true })
const zh = import.meta.glob('./locales/zh-CN/*.json', { eager: true })

function flatten(obj, prefix = '') {
  return Object.entries(obj).flatMap(([k, v]) => {
    const key = prefix ? `${prefix}.${k}` : k
    return v && typeof v === 'object' && !Array.isArray(v) ? flatten(v, key) : [key]
  })
}
const nsKeys = (mods) => {
  const out = {}
  for (const [path, mod] of Object.entries(mods)) {
    const ns = path.split('/').pop().replace('.json', '')
    out[ns] = new Set(flatten(mod.default))
  }
  return out
}

describe('en/zh-CN key parity', () => {
  it('has the same namespaces and keys', () => {
    const e = nsKeys(en), z = nsKeys(zh)
    expect(Object.keys(e).sort()).toEqual(Object.keys(z).sort())
    for (const ns of Object.keys(e)) {
      expect([...z[ns]].sort(), `missing keys in zh-CN/${ns}`).toEqual([...e[ns]].sort())
    }
  })
})
