// Pure helpers for the first-time Semantic DLP enable card (DLPEnableWizard).
// Pre-provision, config lives in config.yaml keys (edited via PUT /api/config);
// validation goes through POST /api/config/test-dlp with candidate values.

const trim = (s) => (s || '').trim()

// distinctChatModels returns the trimmed, non-empty, de-duplicated set of chat
// model ids to validate — the chat model plus a summary model when it differs.
export function distinctChatModels(chatModel, summaryModel) {
  const out = []
  for (const m of [chatModel, summaryModel]) {
    const t = trim(m)
    if (t && !out.includes(t)) out.push(t)
  }
  return out
}

// formComplete gates the Validate button: endpoint + embedding + chat required.
export function formComplete(form) {
  return ['base_url', 'embedding_model', 'chat_model'].every((k) => trim(form[k]) !== '')
}

// buildTestPayload builds the /api/config/test-dlp request. embedding_dim -1
// means "dimension unknown — accept any non-empty vector" (it's auto-detected at
// provision, so there is no dimension to match yet).
export function buildTestPayload(form) {
  return {
    base_url: trim(form.base_url),
    api_key: form.api_key || '',
    embedding_model: trim(form.embedding_model),
    embedding_dim: -1,
    risk_model: trim(form.chat_model),
    chat_models: distinctChatModels(form.chat_model, form.summary_model),
  }
}

// buildEnableChanges builds the PUT /api/config body written just before
// provisioning. The chat model seeds dlp.risk_model (the shared "chat" catalog
// model used by risk/understand/rerank); summary defaults to the chat model.
// api_key is included only when the operator entered one (blank preserves any
// existing value).
export function buildEnableChanges(form) {
  const chat = trim(form.chat_model)
  const summary = trim(form.summary_model) || chat
  const changes = {
    'dlp.base_url': trim(form.base_url),
    'dlp.embedding_model': trim(form.embedding_model),
    'dlp.risk_model': chat,
    'dlp.summary_model': summary,
    'dlp.enabled': true,
  }
  if (trim(form.api_key) !== '') changes['dlp.api_key'] = form.api_key
  return changes
}
