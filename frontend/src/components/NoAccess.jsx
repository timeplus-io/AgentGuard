import { useTranslation } from 'react-i18next'
import Sidebar from './Sidebar'
import TopNav from './TopNav'

// NoAccess is shown when a logged-in user lacks the permission for a page (or
// has no role at all). It keeps the Sidebar (filtered to their allowed pages)
// and TopNav (for logout / theme) so they can navigate elsewhere or sign out.
export default function NoAccess({ what, resource }) {
  const { t } = useTranslation('auth')
  const label = resource
    ? t('common:nav.' + resource, { defaultValue: what || t('noAccess.defaultWhat') })
    : (what || t('noAccess.defaultWhat'))
  return (
    <div className="min-h-screen bg-surface text-gray-100">
      <Sidebar />
      <TopNav />
      <main className="pl-sidebar pt-16 min-h-screen flex items-center justify-center">
        <div className="text-center max-w-md px-6">
          <span className="material-symbols-outlined text-5xl text-gray-400">lock</span>
          <h1 className="text-lg font-semibold mt-4 mb-2">{t('noAccess.title')}</h1>
          <p className="text-sm text-gray-400">
            {t('noAccess.message', { what: label })}
          </p>
        </div>
      </main>
    </div>
  )
}
