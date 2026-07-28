import { useState, useEffect, useCallback, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import PageHeader from '../components/PageHeader'
import { api } from '../api/client'
import { useAuth } from '../context/AuthContext'
import { uiKey, readKey, writeKey } from '../lib/perm'

const inputCls =
  'w-full rounded-sm bg-gray-700 text-gray-100 text-sm px-2 py-1.5 focus:outline-none focus:ring-1 focus:ring-primary'

// Build the full permission set for a set of resources (Full access) or the
// view-only set (ui + read, excluding the access resource).
function fullSet(resources) {
  const s = new Set()
  for (const r of resources) {
    s.add(uiKey(r.key)); s.add(readKey(r.key))
    if (r.has_write) s.add(writeKey(r.key))
  }
  return s
}
function viewSet(resources) {
  const s = new Set()
  for (const r of resources) {
    if (r.key === 'access') continue
    s.add(uiKey(r.key)); s.add(readKey(r.key))
  }
  return s
}

export default function AccessControl() {
  const { t } = useTranslation('auth')
  const { auth, refresh } = useAuth()
  const [tab, setTab] = useState('roles')
  const [catalog, setCatalog] = useState({ resources: [] })
  const [roles, setRoles] = useState([])
  const [users, setUsers] = useState([])
  const [loadErr, setLoadErr] = useState(null)
  const [banner, setBanner] = useState(null)

  const load = useCallback(() => {
    Promise.all([api.getPermissionCatalog(), api.listRoles(), api.listUsers()])
      .then(([cat, r, u]) => {
        setCatalog(cat || { resources: [] })
        setRoles(r.roles || [])
        setUsers(u.users || [])
        setLoadErr(null)
      })
      .catch((e) => setLoadErr(e?.message || t('accessControl.loadError')))
  }, [t])
  useEffect(() => { load() }, [load])

  return (
    <div className="min-h-screen bg-surface text-gray-100">
      <Sidebar />
      <TopNav />
      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 space-y-6 max-w-5xl">
          <PageHeader title={t('accessControl.title')} subtitle={t('accessControl.subtitle')} />

          {loadErr && <div className="bg-error-container/40 text-error text-sm rounded-sm px-4 py-2">{loadErr}</div>}
          {banner && (
            <div className={`text-sm rounded-sm px-4 py-2 ${banner.type === 'err' ? 'bg-error-container/40 text-error' : 'bg-green-500/10 text-green-400'}`}>
              {banner.msg}
            </div>
          )}

          <div className="flex gap-1">
            {['roles', 'users'].map((tabKey) => (
              <button
                key={tabKey}
                onClick={() => setTab(tabKey)}
                className={`px-4 py-2 text-xs font-semibold rounded-sm capitalize transition-colors ${
                  tab === tabKey ? 'bg-gray-700 text-primary' : 'text-gray-400 hover:bg-gray-700'
                }`}
              >
                {t(`accessControl.tabs.${tabKey}`)}
              </button>
            ))}
          </div>

          {tab === 'roles' ? (
            <RolesTab catalog={catalog} roles={roles} onChanged={() => { load(); refresh() }} setBanner={setBanner} />
          ) : (
            <UsersTab
              roles={roles} users={users} me={auth.username}
              onChanged={() => { load(); refresh() }} setBanner={setBanner}
            />
          )}
        </div>
      </main>
    </div>
  )
}

// ── Roles ─────────────────────────────────────────────────────────────────────

function RolesTab({ catalog, roles, onChanged, setBanner }) {
  const { t } = useTranslation('auth')
  const [editing, setEditing] = useState(null) // {name, description, perms:Set, builtin, isNew}
  const resources = catalog.resources || []

  const startNew = () => setEditing({ name: '', description: '', perms: new Set(), builtin: false, isNew: true })
  const startEdit = (r) =>
    setEditing({ name: r.name, description: r.description || '', perms: new Set(r.permissions || []), builtin: r.builtin, isNew: false })

  const remove = async (r) => {
    if (!window.confirm(t('accessControl.role.deleteConfirm', { name: r.name }))) return
    try {
      await api.deleteRole(r.name)
      setBanner({ type: 'ok', msg: t('accessControl.role.deletedMsg', { name: r.name }) })
      onChanged()
    } catch (e) {
      setBanner({ type: 'err', msg: e?.message || t('accessControl.role.deleteFailed') })
    }
  }

  return (
    <div className="space-y-6">
      {!editing && (
        <>
          <div className="flex justify-end">
            <button onClick={startNew} className="px-4 py-2 text-xs font-semibold rounded-sm bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023]">
              {t('accessControl.role.newRole')}
            </button>
          </div>
          <div className="space-y-2">
            {roles.map((r) => (
              <div key={r.name} className="bg-surface-container-high rounded-sm px-4 py-3 flex items-center gap-3">
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-semibold text-gray-100">{r.name}</span>
                    {r.builtin && <span className="text-[10px] uppercase tracking-wide bg-gray-700 text-gray-400 rounded-full px-2 py-0.5">{t('accessControl.role.builtinBadge')}</span>}
                  </div>
                  {r.description && <p className="text-xs text-gray-400 mt-0.5 truncate">{r.description}</p>}
                  <p className="text-[11px] text-gray-500 mt-0.5">{t('accessControl.role.permissionsCount', { count: (r.permissions || []).length })}</p>
                </div>
                <button onClick={() => startEdit(r)} className="text-xs text-gray-400 hover:text-gray-100 px-2 py-1">
                  {r.builtin ? t('accessControl.role.view') : t('accessControl.role.edit')}
                </button>
                {!r.builtin && (
                  <button onClick={() => remove(r)} className="text-xs text-error hover:opacity-80 px-2 py-1">{t('accessControl.role.delete')}</button>
                )}
              </div>
            ))}
          </div>
        </>
      )}

      {editing && (
        <RoleEditor
          resources={resources} editing={editing} setEditing={setEditing}
          onDone={() => { setEditing(null); onChanged() }}
          setBanner={setBanner}
        />
      )}
    </div>
  )
}

function RoleEditor({ resources, editing, setEditing, onDone, setBanner }) {
  const { t } = useTranslation('auth')
  const readOnly = editing.builtin
  const has = (k) => editing.perms.has(k)
  const toggle = (k) => {
    if (readOnly) return
    setEditing((e) => {
      const perms = new Set(e.perms)
      perms.has(k) ? perms.delete(k) : perms.add(k)
      return { ...e, perms }
    })
  }
  const setAll = (build) => !readOnly && setEditing((e) => ({ ...e, perms: build(resources) }))

  const save = async () => {
    const name = (editing.name || '').trim()
    if (!name) { setBanner({ type: 'err', msg: t('accessControl.editor.nameRequired') }); return }
    const payload = { name, description: editing.description, permissions: [...editing.perms] }
    try {
      if (editing.isNew) await api.createRole(payload)
      else await api.updateRole(name, payload)
      setBanner({ type: 'ok', msg: t('accessControl.editor.savedMsg', { name }) })
      onDone()
    } catch (e) {
      setBanner({ type: 'err', msg: e?.message || t('accessControl.editor.saveFailed') })
    }
  }

  return (
    <div className="bg-surface-container-high rounded-sm p-5 space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold text-gray-100">
          {editing.builtin
            ? t('accessControl.editor.builtinTitle', { name: editing.name })
            : editing.isNew
              ? t('accessControl.editor.newTitle')
              : t('accessControl.editor.editTitle', { name: editing.name })}
        </h3>
        <button onClick={() => setEditing(null)} className="text-xs text-gray-400 hover:text-gray-100">{t('accessControl.editor.back')}</button>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <label className="space-y-1">
          <span className="text-[11px] uppercase tracking-wide text-gray-400">{t('accessControl.editor.nameLabel')}</span>
          <input
            className={inputCls} value={editing.name}
            disabled={!editing.isNew || readOnly}
            onChange={(e) => setEditing((s) => ({ ...s, name: e.target.value }))}
            placeholder={t('accessControl.editor.namePlaceholder')}
          />
        </label>
        <label className="space-y-1">
          <span className="text-[11px] uppercase tracking-wide text-gray-400">{t('accessControl.editor.descriptionLabel')}</span>
          <input
            className={inputCls} value={editing.description} disabled={readOnly}
            onChange={(e) => setEditing((s) => ({ ...s, description: e.target.value }))}
          />
        </label>
      </div>

      {!readOnly && (
        <div className="flex gap-2 text-xs">
          <button onClick={() => setAll(fullSet)} className="px-2 py-1 rounded-sm bg-gray-700 text-gray-300 hover:text-gray-100">{t('accessControl.editor.fullAccess')}</button>
          <button onClick={() => setAll(viewSet)} className="px-2 py-1 rounded-sm bg-gray-700 text-gray-300 hover:text-gray-100">{t('accessControl.editor.viewOnly')}</button>
          <button onClick={() => setEditing((e) => ({ ...e, perms: new Set() }))} className="px-2 py-1 rounded-sm bg-gray-700 text-gray-300 hover:text-gray-100">{t('accessControl.editor.clear')}</button>
        </div>
      )}

      {/* Permission matrix */}
      <div className="overflow-x-auto">
        <table className="text-xs w-full">
          <thead>
            <tr className="text-gray-400 text-left">
              <th className="py-2 pr-4 font-semibold">{t('accessControl.editor.table.resource')}</th>
              <th className="py-2 px-3 font-semibold text-center">{t('accessControl.editor.table.ui')}</th>
              <th className="py-2 px-3 font-semibold text-center">{t('accessControl.editor.table.apiRead')}</th>
              <th className="py-2 px-3 font-semibold text-center">{t('accessControl.editor.table.apiWrite')}</th>
            </tr>
          </thead>
          <tbody>
            {resources.map((r) => (
              <tr key={r.key} className="border-t border-outline-variant/10">
                <td className="py-2 pr-4">
                  <span className="text-gray-100">{r.label}</span>
                  <span className="text-gray-500 ml-2">{r.group}</span>
                </td>
                <td className="py-2 px-3 text-center"><Cell checked={has(uiKey(r.key))} onChange={() => toggle(uiKey(r.key))} disabled={readOnly} /></td>
                <td className="py-2 px-3 text-center"><Cell checked={has(readKey(r.key))} onChange={() => toggle(readKey(r.key))} disabled={readOnly} /></td>
                <td className="py-2 px-3 text-center">
                  {r.has_write
                    ? <Cell checked={has(writeKey(r.key))} onChange={() => toggle(writeKey(r.key))} disabled={readOnly} />
                    : <span className="text-gray-600">—</span>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {!readOnly && (
        <div className="flex justify-end">
          <button onClick={save} className="px-4 py-2 text-xs font-semibold rounded-sm bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023]">
            {t('accessControl.editor.save')}
          </button>
        </div>
      )}
    </div>
  )
}

function Cell({ checked, onChange, disabled }) {
  return (
    <input type="checkbox" checked={checked} onChange={onChange} disabled={disabled}
      className="w-4 h-4 accent-[#f256a3] cursor-pointer disabled:cursor-not-allowed disabled:opacity-60" />
  )
}

// ── Users ─────────────────────────────────────────────────────────────────────

function UsersTab({ roles, users, me, onChanged, setBanner }) {
  const { t } = useTranslation('auth')
  const [newUser, setNewUser] = useState('')
  const [newRole, setNewRole] = useState('')
  const roleNames = useMemo(() => roles.map((r) => r.name), [roles])

  const assign = async (user, role) => {
    try {
      await api.assignUserRole(user, role)
      setBanner({ type: 'ok', msg: role ? t('accessControl.users.assignedMsg', { role, user }) : t('accessControl.users.removedMsg', { user }) })
      onChanged()
    } catch (e) {
      setBanner({ type: 'err', msg: e?.message || t('accessControl.users.assignFailed') })
    }
  }

  const addUser = async () => {
    const u = newUser.trim()
    if (!u || !newRole) { setBanner({ type: 'err', msg: t('accessControl.users.missingFields') }); return }
    await assign(u, newRole)
    setNewUser(''); setNewRole('')
  }

  return (
    <div className="space-y-6">
      {/* Add a user by name (may not have logged in yet) */}
      <div className="bg-surface-container-high rounded-sm p-4 flex flex-wrap items-end gap-3">
        <label className="space-y-1 flex-1 min-w-[180px]">
          <span className="text-[11px] uppercase tracking-wide text-gray-400">{t('accessControl.users.usernameLabel')}</span>
          <input className={inputCls} value={newUser} onChange={(e) => setNewUser(e.target.value)} placeholder={t('accessControl.users.usernamePlaceholder')} />
        </label>
        <label className="space-y-1 min-w-[160px]">
          <span className="text-[11px] uppercase tracking-wide text-gray-400">{t('accessControl.users.roleLabel')}</span>
          <select className={inputCls} value={newRole} onChange={(e) => setNewRole(e.target.value)}>
            <option value="">{t('accessControl.users.roleSelectPlaceholder')}</option>
            {roleNames.map((n) => <option key={n} value={n}>{n}</option>)}
          </select>
        </label>
        <button onClick={addUser} className="px-4 py-2 text-xs font-semibold rounded-sm bg-gray-700 text-gray-100 hover:bg-secondary-container">
          {t('accessControl.users.assign')}
        </button>
      </div>

      <div className="space-y-2">
        {users.length === 0 && <p className="text-sm text-gray-400">{t('accessControl.users.empty')}</p>}
        {users.map((u) => (
          <div key={u.user} className="bg-surface-container-high rounded-sm px-4 py-3 flex items-center gap-3">
            <div className="flex-1 min-w-0">
              <span className="text-sm font-semibold text-gray-100">{u.user}</span>
              {u.user === me && <span className="text-[10px] text-gray-500 ml-2">{t('accessControl.users.you')}</span>}
              {!roleNames.includes(u.role) && (
                <span className="text-[10px] text-error ml-2">{t('accessControl.users.roleGone', { role: u.role })}</span>
              )}
            </div>
            <select
              className="rounded-sm bg-gray-700 text-gray-100 text-xs px-2 py-1.5"
              value={roleNames.includes(u.role) ? u.role : ''}
              onChange={(e) => assign(u.user, e.target.value)}
            >
              <option value="">{t('accessControl.users.noAccessOption')}</option>
              {roleNames.map((n) => <option key={n} value={n}>{n}</option>)}
            </select>
          </div>
        ))}
      </div>
    </div>
  )
}
