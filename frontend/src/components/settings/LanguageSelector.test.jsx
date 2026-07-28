import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import i18n from '../../i18n'
import LanguageSelector from './LanguageSelector.jsx'

beforeEach(async () => { localStorage.clear(); await i18n.changeLanguage('en') })

describe('LanguageSelector', () => {
  it('switches the app language and persists the choice', () => {
    render(<LanguageSelector />)
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'zh-CN' } })
    expect(i18n.language).toBe('zh-CN')
    expect(localStorage.getItem('agentguard.locale')).toBe('zh-CN')
  })
})
