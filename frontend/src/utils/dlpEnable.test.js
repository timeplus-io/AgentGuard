import { describe, it, expect } from 'vitest'
import {
  distinctChatModels,
  formComplete,
  buildTestPayload,
  buildEnableChanges,
} from './dlpEnable.js'

describe('distinctChatModels', () => {
  it('dedupes and drops blanks, preserving order', () => {
    expect(distinctChatModels('chat', 'chat')).toEqual(['chat'])
    expect(distinctChatModels('chat', 'sum')).toEqual(['chat', 'sum'])
    expect(distinctChatModels('chat', '')).toEqual(['chat'])
    expect(distinctChatModels(' a ', 'b')).toEqual(['a', 'b'])
  })
})

describe('formComplete', () => {
  const full = { base_url: 'u', embedding_model: 'e', chat_model: 'c' }
  it('requires base_url, embedding_model, chat_model', () => {
    expect(formComplete(full)).toBe(true)
    expect(formComplete({ ...full, base_url: '' })).toBe(false)
    expect(formComplete({ ...full, embedding_model: '  ' })).toBe(false)
    expect(formComplete({ ...full, chat_model: '' })).toBe(false)
  })
})

describe('buildTestPayload', () => {
  it('sends embedding_dim -1 and deduped chat_models', () => {
    const p = buildTestPayload({
      base_url: 'u', api_key: 'k', embedding_model: 'e',
      chat_model: 'c', summary_model: 'c',
    })
    expect(p).toEqual({
      base_url: 'u', api_key: 'k', embedding_model: 'e',
      embedding_dim: -1, risk_model: 'c', chat_models: ['c'],
    })
  })
  it('includes a distinct summary model', () => {
    const p = buildTestPayload({
      base_url: 'u', api_key: '', embedding_model: 'e',
      chat_model: 'c', summary_model: 's',
    })
    expect(p.chat_models).toEqual(['c', 's'])
  })
})

describe('buildEnableChanges', () => {
  it('maps chat->risk_model, defaults summary to chat, enables', () => {
    const c = buildEnableChanges({
      base_url: 'u', api_key: '', embedding_model: 'e',
      chat_model: 'c', summary_model: '',
    })
    expect(c).toEqual({
      'dlp.base_url': 'u',
      'dlp.embedding_model': 'e',
      'dlp.risk_model': 'c',
      'dlp.summary_model': 'c',
      'dlp.enabled': true,
    })
  })
  it('includes api_key only when non-blank', () => {
    const c = buildEnableChanges({
      base_url: 'u', api_key: 'sk-1', embedding_model: 'e',
      chat_model: 'c', summary_model: 's',
    })
    expect(c['dlp.api_key']).toBe('sk-1')
    expect(c['dlp.summary_model']).toBe('s')
  })
})
