import { describe, it, expect } from 'vitest'
import i18n from './index.js'

describe('i18n init', () => {
  it('defaults to English and resolves a common key', () => {
    expect(i18n.t('common:actions.save')).toBe('Save')
  })
  it('switches to Simplified Chinese', async () => {
    await i18n.changeLanguage('zh-CN')
    expect(i18n.t('common:actions.save')).toBe('保存')
    await i18n.changeLanguage('en')
  })
})
