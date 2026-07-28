import { describe, it, expect } from 'vitest'

const en = import.meta.glob('./locales/en/*.json', { eager: true })
const zh = import.meta.glob('./locales/zh-CN/*.json', { eager: true })
const load = (mods, ns) =>
  Object.entries(mods).find(([p]) => p.endsWith(`/${ns}.json`))?.[1].default

// Representative keys appended by each page task. Each entry: [ns, dottedKey].
export const REPRESENTATIVE = [
  ['common', 'actions.save'],
  ['settings', 'language.label'],
]

describe('zh-CN is actually translated', () => {
  it('representative keys differ from English and are non-empty', () => {
    for (const [ns, key] of REPRESENTATIVE) {
      const e = key.split('.').reduce((o, k) => o?.[k], load(en, ns))
      const z = key.split('.').reduce((o, k) => o?.[k], load(zh, ns))
      expect(z, `${ns}.${key} empty`).toBeTruthy()
      expect(z, `${ns}.${key} not translated`).not.toBe(e)
    }
  })
})
