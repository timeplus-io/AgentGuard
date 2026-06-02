import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { AuthProvider, useAuth } from './context/AuthContext'
import { NotificationsProvider } from './components/NotificationsProvider'
import HoldToastContainer from './components/HoldToast'
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
import Onboarding from './pages/Onboarding'
import BootStatus from './pages/BootStatus'

function AuthGuard({ children }) {
  const { auth, refresh } = useAuth()
  if (auth.loading) {
    return (
      <div className="min-h-screen bg-gray-900 flex items-center justify-center">
        <span className="text-gray-100 text-sm">Loading…</span>
      </div>
    )
  }
  const sup = auth.supervisor
  if (sup && sup.state !== 'ready' && sup.state !== 'disabled') {
    return <BootStatus status={sup} onReady={refresh} />
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

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <AuthGuard>
          <Routes>
            <Route path="/" element={<Navigate to="/dashboard" replace />} />
            <Route path="/dashboard" element={<Dashboard />} />
            <Route path="/agents" element={<AgentList />} />
            <Route path="/agents/:id" element={<AgentDetail />} />
            <Route path="/threats" element={<Threats />} />
            <Route path="/threats/:id" element={<ThreatDetail />} />
            <Route path="/threats/by-hold/:id" element={<ThreatDetail />} />
            <Route path="/cost" element={<CostGovernance />} />
            <Route path="/rules" element={<RulePacks />} />
            <Route path="/rules/new" element={<RuleCreate />} />
            <Route path="/rules/:id" element={<RuleDetail />} />
            <Route path="/investigate" element={<Investigate />} />
            <Route path="/onboarding" element={<Onboarding />} />
          </Routes>
        </AuthGuard>
      </AuthProvider>
    </BrowserRouter>
  )
}
