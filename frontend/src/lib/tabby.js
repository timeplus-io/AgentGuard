export const OPEN_TABBY_EVENT = 'agentguard:open-tabby'

export function openTabby() {
  window.dispatchEvent(new CustomEvent(OPEN_TABBY_EVENT))
}
