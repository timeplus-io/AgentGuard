import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import Dashboard from './pages/Dashboard'
import AgentList from './pages/AgentList'
import AgentDetail from './pages/AgentDetail'
import Threats from './pages/Threats'
import CostGovernance from './pages/CostGovernance'
import RulePacks from './pages/RulePacks'
import Investigate from './pages/Investigate'
import Onboarding from './pages/Onboarding'
import Settings from './pages/Settings'

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Navigate to="/dashboard" replace />} />
        <Route path="/dashboard" element={<Dashboard />} />
        <Route path="/agents" element={<AgentList />} />
        <Route path="/agents/:id" element={<AgentDetail />} />
        <Route path="/threats" element={<Threats />} />
        <Route path="/cost" element={<CostGovernance />} />
        <Route path="/rules" element={<RulePacks />} />
        <Route path="/investigate" element={<Investigate />} />
        <Route path="/onboarding" element={<Onboarding />} />
        <Route path="/settings" element={<Settings />} />
      </Routes>
    </BrowserRouter>
  )
}
