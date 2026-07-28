import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { AuthProvider, useAuth } from './context/AuthContext'
import { NotificationsProvider } from './components/NotificationsProvider'
import HoldToastContainer from './components/HoldToast'
import NoAccess from './components/NoAccess'
import { uiKey, NAV_RESOURCES } from './lib/perm'
import Login from './pages/Login'
import Setup from './pages/Setup'
import Dashboard from './pages/Dashboard'
import AgentList from './pages/AgentList'
import AgentDetail from './pages/AgentDetail'
import Threats from './pages/Threats'
import ThreatDetail from './pages/ThreatDetail'
import CostGovernance from './pages/CostGovernance'
import RulePacks from './pages/RulePacks'
import RuleDetail from './pages/RuleDetail'
import RuleCreate from './pages/RuleCreate'
import Investigate from './pages/Investigate'
import SemanticDLP from './pages/SemanticDLP'
import Sentry from './pages/Sentry'
import Settings from './pages/Settings'
import Onboarding from './pages/Onboarding'
import AccessControl from './pages/AccessControl'

function AuthGuard({ children }) {
  const { auth } = useAuth()
  if (auth.loading) {
    return (
      <div className="min-h-screen bg-gray-900 flex items-center justify-center">
        <span className="text-gray-100 text-sm">Loading…</span>
      </div>
    )
  }
  if (!auth.setupComplete) return <Setup />
  if (!auth.loggedIn) return <Login />
  // NotificationsProvider opens an SSE stream that requires auth — only mount when logged in.
  return (
    <NotificationsProvider>
      {children}
      <HoldToastContainer />
    </NotificationsProvider>
  )
}

// Gate renders its page only if the user holds ui:<resource>; otherwise it
// shows the NoAccess screen (which keeps the nav so they can move elsewhere).
function Gate({ resource, what, children }) {
  const { can } = useAuth()
  return can(uiKey(resource)) ? children : <NoAccess what={what} />
}

// HomeRedirect sends the user to their first accessible page (so a limited role
// lands somewhere useful), or the NoAccess screen when they have none.
function HomeRedirect() {
  const { can } = useAuth()
  const target = NAV_RESOURCES.find(([r]) => can(uiKey(r)))
  return target ? <Navigate to={target[1]} replace /> : <NoAccess what="AgentGuard" />
}

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <AuthGuard>
          <Routes>
            <Route path="/" element={<HomeRedirect />} />
            <Route path="/dashboard" element={<Gate resource="dashboard" what="the dashboard"><Dashboard /></Gate>} />
            <Route path="/agents" element={<Gate resource="agents" what="agents"><AgentList /></Gate>} />
            <Route path="/agents/:id" element={<Gate resource="agents" what="agents"><AgentDetail /></Gate>} />
            <Route path="/threats" element={<Gate resource="threats" what="threats"><Threats /></Gate>} />
            <Route path="/threats/:id" element={<Gate resource="threats" what="threats"><ThreatDetail /></Gate>} />
            <Route path="/threats/by-hold/:id" element={<Gate resource="threats" what="threats"><ThreatDetail /></Gate>} />
            <Route path="/cost" element={<Gate resource="cost" what="cost governance"><CostGovernance /></Gate>} />
            <Route path="/rules" element={<Gate resource="rules" what="rules"><RulePacks /></Gate>} />
            <Route path="/rules/new" element={<Gate resource="rules" what="rules"><RuleCreate mode="create" /></Gate>} />
            <Route path="/rules/:id/edit" element={<Gate resource="rules" what="rules"><RuleCreate mode="edit" /></Gate>} />
            <Route path="/rules/:id" element={<Gate resource="rules" what="rules"><RuleDetail /></Gate>} />
            <Route path="/investigate" element={<Gate resource="investigate" what="the SQL console"><Investigate /></Gate>} />
            <Route path="/dlp" element={<Gate resource="dlp" what="Semantic DLP"><SemanticDLP /></Gate>} />
            <Route path="/sentry" element={<Gate resource="sentry" what="Sentry"><Sentry /></Gate>} />
            <Route path="/settings" element={<Gate resource="settings" what="settings"><Settings /></Gate>} />
            <Route path="/access" element={<Gate resource="access" what="access control"><AccessControl /></Gate>} />
            <Route path="/onboarding" element={<Gate resource="settings" what="onboarding"><Onboarding /></Gate>} />
          </Routes>
        </AuthGuard>
      </AuthProvider>
    </BrowserRouter>
  )
}
