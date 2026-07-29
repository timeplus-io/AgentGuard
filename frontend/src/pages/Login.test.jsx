import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import '../i18n'

vi.mock('../api/client', () => ({
  api: { authLogin: vi.fn(), authStatus: vi.fn() },
}))

import { api } from '../api/client'
import { AuthProvider, useAuth } from '../context/AuthContext'
import Login from './Login.jsx'

// Renders the current permission set so the test can observe the auth context.
function Perms() {
  const { auth } = useAuth()
  return <div data-testid="perms">{(auth.permissions || []).join(',')}</div>
}

beforeEach(() => { localStorage.clear(); vi.clearAllMocks() })

describe('Login', () => {
  it('loads the new user\'s permissions after login (no stale previous-user UI)', async () => {
    // First authStatus (AuthProvider mount) = previous user; second (post-login) = new user.
    api.authStatus
      .mockResolvedValueOnce({ setup_complete: true, logged_in: true, username: 'alice', role: 'admin', permissions: ['ui:settings'], rbac_active: true })
      .mockResolvedValueOnce({ setup_complete: true, logged_in: true, username: 'bob', role: 'viewer', permissions: ['ui:dashboard'], rbac_active: true })
    api.authLogin.mockResolvedValue({ username: 'bob' })

    render(
      <AuthProvider>
        <Login />
        <Perms />
      </AuthProvider>,
    )

    // Previous user's permissions are in context.
    await waitFor(() => expect(screen.getByTestId('perms').textContent).toBe('ui:settings'))

    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'bob' } })
    fireEvent.click(screen.getByRole('button'))

    // After logging in as a different user, the context must reflect the new
    // user's permissions — not the previous user's, which required a hard reload.
    await waitFor(() => expect(screen.getByTestId('perms').textContent).toBe('ui:dashboard'))
  })
})
