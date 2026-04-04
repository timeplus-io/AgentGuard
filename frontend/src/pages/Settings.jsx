import { useState, useEffect } from 'react'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import PageHeader from '../components/PageHeader'
import Toggle from '../components/Toggle'
import { api } from '../api/client'

const tabs = ['General', 'Integrations', 'Users & RBAC']

const integrations = [
  { id: 'slack', name: 'Slack', desc: 'Incident alerting and automated threat reporting to your security channels.', icon: 'chat', connected: true },
  { id: 'pagerduty', name: 'PagerDuty', desc: 'On-call scheduling and high-priority incident escalation for critical agents.', icon: 'notifications_active', connected: true },
  { id: 'webhooks', name: 'Webhooks', desc: 'Outgoing event notifications to custom HTTP endpoints for flexible automation.', icon: 'webhook', connected: false },
  { id: 'splunk', name: 'Splunk', desc: 'Forward security logs and agent performance metrics for SIEM analysis.', icon: 'analytics', connected: true },
  { id: 'iceberg', name: 'Apache Iceberg', desc: 'High-performance table format for storing large-scale historical security data.', icon: 'database', connected: false },
]

const users = [
  { name: 'Marcus Thorne', email: 'm.thorne@agentshield.ai', role: 'Admin', status: 'Active', statusDot: 'bg-[#ffb0cf]', roleClass: 'bg-[#f256a3]/20 text-[#ffb0cf] border border-[#ffb0cf]/20' },
  { name: 'Elena Rodriguez', email: 'elena.r@agentshield.ai', role: 'Analyst', status: 'Active', statusDot: 'bg-[#ffb0cf]', roleClass: 'bg-[#4b4754]/30 text-[#cbc4d4] border border-[#cbc4d4]/20' },
  { name: 'Julian Voss', email: 'jvoss@agentshield.ai', role: 'Viewer', status: 'Offline', statusDot: 'bg-zinc-600', roleClass: 'bg-[#37333f] text-zinc-400 border border-[#574149]/20' },
  { name: 'Sarah Chen', email: 's.chen@agentshield.ai', role: 'Analyst', status: 'Invited', statusDot: 'bg-[#ff506f] shadow-[0_0_8px_rgba(255,80,111,0.5)]', roleClass: 'bg-[#4b4754]/30 text-[#cbc4d4] border border-[#cbc4d4]/20' },
]

const rbacCards = [
  { icon: 'verified_user', iconColor: 'text-[#ffb0cf]', border: 'border-[#ffb0cf]/40', title: 'Admin Privileges', desc: 'Full access to system configuration, agent deployment, and user management. Can terminate agents and modify global security rules.' },
  { icon: 'visibility', iconColor: 'text-[#cbc4d4]', border: 'border-[#cbc4d4]/40', title: 'Analyst Privileges', desc: 'Access to threat detection streams, investigation tools, and cost reports. Can flag suspicious activities and adjust individual agent thresholds.' },
  { icon: 'lock_open', iconColor: 'text-zinc-500', border: 'border-[#a58a93]/40', title: 'Viewer Privileges', desc: 'Read-only access to global health dashboards and summary reports. Useful for executive oversight without operational intervention capability.' },
]

export default function Settings() {
  const [tab, setTab] = useState('General')
  const [settings, setSettings] = useState(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [orgName, setOrgName] = useState('Quantum Defenses Ltd.')
  const [domain, setDomain] = useState('quantum.shield')
  const [email, setEmail] = useState('security-ops@quantum.shield')
  const [hotDays, setHotDays] = useState(14)
  const [warmDays, setWarmDays] = useState(90)
  const [coldDays, setColdDays] = useState(365)
  const [autoPatch, setAutoPatch] = useState(true)
  const [aiHeuristic, setAiHeuristic] = useState(false)
  const [egressFilter, setEgressFilter] = useState(true)

  useEffect(() => {
    api.getSettings()
      .then(data => {
        if (data) {
          setOrgName(data.orgName || orgName)
          setDomain(data.domain || domain)
          setEmail(data.email || email)
          setHotDays(data.hotDays || hotDays)
          setWarmDays(data.warmDays || warmDays)
          setColdDays(data.coldDays || coldDays)
        }
        setSettings(data)
      })
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  const handleSave = async () => {
    setSaving(true)
    try {
      await api.updateSettings({ orgName, domain, email, hotDays, warmDays, coldDays, autoPatch, aiHeuristic, egressFilter })
    } catch {}
    setSaving(false)
  }

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />
      <Footer />
      <main className="pl-64 pt-16 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 space-y-8">
          <PageHeader
            title="System Settings"
            subtitle="Configure organization profile, integrations, access control, and security policies."
            actions={
              <button onClick={handleSave} disabled={saving} className="px-4 py-2 bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] text-xs font-semibold rounded-sm disabled:opacity-50">
                {saving ? 'Saving...' : 'Save Changes'}
              </button>
            }
          />
        <div className="max-w-7xl w-full">
          <div className="grid grid-cols-12 gap-8">
            {/* Sidebar Nav */}
            <div className="col-span-3 flex flex-col gap-1">
              <h3 className="text-[10px] uppercase tracking-widest text-zinc-500 font-semibold mb-4 px-4">Navigation</h3>
              {[
                { label: 'General', icon: 'chevron_right' },
                { label: 'Integrations', icon: 'link' },
                { label: 'Users & RBAC', icon: 'group' },
                { label: 'Billing', icon: 'receipt_long' },
              ].map(item => (
                <button
                  key={item.label}
                  onClick={() => setTab(item.label)}
                  className={`flex items-center justify-between px-4 py-2 rounded-sm transition-all ${
                    tab === item.label
                      ? 'bg-[#2c2834] text-[#ffb0cf] border-r-2 border-[#ffb0cf]'
                      : 'text-zinc-400 hover:bg-[#211e29]/50 hover:text-white'
                  }`}
                >
                  <span className="text-xs font-medium">{item.label}</span>
                  <span className="material-symbols-outlined text-xs">{item.icon}</span>
                </button>
              ))}
            </div>

            {/* Content */}
            <div className="col-span-9 space-y-12">

              {/* General */}
              {tab === 'General' && (
                <>
                  <section>
                    <h4 className="text-xl font-medium mb-6 tracking-tight">Organization Profile</h4>
                    <div className="bg-[#211e29] p-6 rounded-sm space-y-6">
                      <div className="grid grid-cols-2 gap-6">
                        <div className="space-y-2">
                          <label className="text-xs font-semibold text-zinc-400 uppercase tracking-widest">Organization Name</label>
                          <input value={orgName} onChange={e => setOrgName(e.target.value)} className="w-full bg-[#0f0c17] border-none text-[#e7dff0] focus:ring-1 focus:ring-[#ffb0cf] rounded-sm py-2 px-3 text-xs" type="text" />
                        </div>
                        <div className="space-y-2">
                          <label className="text-xs font-semibold text-zinc-400 uppercase tracking-widest">Primary Domain</label>
                          <input value={domain} onChange={e => setDomain(e.target.value)} className="w-full bg-[#0f0c17] border-none text-[#e7dff0] focus:ring-1 focus:ring-[#ffb0cf] rounded-sm py-2 px-3 text-xs" type="text" />
                        </div>
                      </div>
                      <div className="space-y-2">
                        <label className="text-xs font-semibold text-zinc-400 uppercase tracking-widest">Support Contact Email</label>
                        <input value={email} onChange={e => setEmail(e.target.value)} className="w-full bg-[#0f0c17] border-none text-[#e7dff0] focus:ring-1 focus:ring-[#ffb0cf] rounded-sm py-2 px-3 text-xs" type="email" />
                      </div>
                    </div>
                  </section>

                  <section>
                    <div className="flex justify-between items-end mb-6">
                      <div>
                        <h4 className="text-xl font-medium tracking-tight">Data Retention & TTL</h4>
                        <p className="text-xs text-zinc-500 mt-1">Configure automated lifecycle policies for security telemetry.</p>
                      </div>
                      <span className="text-[10px] font-semibold text-[#f256a3] px-2 py-1 bg-[#ffb0cf]/10 rounded-full tracking-widest">REAL-TIME SYNC</span>
                    </div>
                    <div className="grid grid-cols-3 gap-4">
                      {[
                        { icon: 'local_fire_department', iconColor: 'text-[#ffb0cf]', label: 'Hot Storage', desc: 'Instant access, high performance for active threat hunting.', val: hotDays, setVal: setHotDays, borderTop: 'border-t-2 border-[#ffb0cf]' },
                        { icon: 'thermostat', iconColor: 'text-[#cbc4d4]', label: 'Warm Storage', desc: 'Optimized for historical analysis and investigation forensics.', val: warmDays, setVal: setWarmDays, borderTop: '' },
                        { icon: 'ac_unit', iconColor: 'text-zinc-500', label: 'Cold Archival', desc: 'Encrypted deep-storage for regulatory compliance audits.', val: coldDays, setVal: setColdDays, borderTop: '' },
                      ].map(s => (
                        <div key={s.label} className={`bg-[#2c2834] p-5 rounded-sm ${s.borderTop}`}>
                          <div className="flex items-center gap-2 mb-4">
                            <span className={`material-symbols-outlined ${s.iconColor} text-lg`}>{s.icon}</span>
                            <h5 className="text-xs font-semibold uppercase tracking-wider">{s.label}</h5>
                          </div>
                          <p className="text-[10px] text-zinc-500 leading-relaxed mb-4">{s.desc}</p>
                          <div className="flex items-center gap-2">
                            <input value={s.val} onChange={e => s.setVal(Number(e.target.value))} className="w-16 bg-[#0f0c17] border-none text-[#e7dff0] focus:ring-1 focus:ring-[#ffb0cf] rounded-sm py-1.5 px-2 text-xs text-center" type="number" />
                            <span className="text-xs text-zinc-400 font-medium">Days</span>
                          </div>
                        </div>
                      ))}
                    </div>
                  </section>

                  <section>
                    <h4 className="text-xl font-medium mb-6 tracking-tight">Global Security Defaults</h4>
                    <div className="bg-[#211e29] rounded-sm overflow-hidden divide-y divide-zinc-800/50">
                      {[
                        { icon: 'security_update_good', label: 'Automated Agent Patching', desc: 'Deploy latest security kernels without maintenance windows.', val: autoPatch, set: setAutoPatch },
                        { icon: 'psychology', label: 'AI Heuristic Override', desc: 'Allow autonomous agents to terminate non-compliant network processes.', val: aiHeuristic, set: setAiHeuristic },
                        { icon: 'policy', label: 'Aggressive Egress Filtering', desc: 'Block all unknown traffic patterns originating from internal nodes.', val: egressFilter, set: setEgressFilter },
                      ].map(item => (
                        <div key={item.label} className="p-6 flex items-center justify-between">
                          <div className="flex gap-4">
                            <div className="p-2 bg-[#37333f] rounded-sm">
                              <span className="material-symbols-outlined text-[#ffb0cf]">{item.icon}</span>
                            </div>
                            <div>
                              <p className="font-medium text-xs">{item.label}</p>
                              <p className="text-xs text-zinc-500">{item.desc}</p>
                            </div>
                          </div>
                          <Toggle checked={item.val} onChange={item.set} />
                        </div>
                      ))}
                    </div>
                  </section>

                  <div className="flex justify-end gap-4 pt-8">
                    <button className="px-6 py-2 text-xs font-medium text-zinc-400 hover:text-white transition-colors">Discard Changes</button>
                    <button onClick={handleSave} disabled={saving} className="px-8 py-2 text-xs font-semibold bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] rounded-sm shadow-[0_0_20px_rgba(213,63,140,0.2)] hover:shadow-[0_0_25px_rgba(213,63,140,0.4)] transition-all disabled:opacity-70">
                      {saving ? 'Saving...' : 'Save Configurations'}
                    </button>
                  </div>
                </>
              )}

              {/* Integrations */}
              {tab === 'Integrations' && (
                <>
                  <div>
                    <h2 className="text-3xl font-medium text-[#e7dff0] tracking-tight mb-4">Integrations</h2>
                    <p className="text-zinc-500 text-sm max-w-2xl leading-relaxed">Connect your security stack to AgentShield. Orchestrate real-time threat detection and automated response workflows across your entire infrastructure.</p>
                  </div>
                  <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
                    {integrations.map(intg => (
                      <div key={intg.id} className="bg-[#1d1a25] p-6 rounded-sm group hover:bg-[#2c2834] transition-colors duration-300 flex flex-col justify-between min-h-[220px]">
                        <div className="flex justify-between items-start">
                          <div className="w-12 h-12 bg-[#2c2834] rounded-sm flex items-center justify-center border border-[#574149]/10">
                            <span className={`material-symbols-outlined text-2xl ${intg.connected ? 'text-[#ffb0cf]' : 'text-zinc-500'}`}>{intg.icon}</span>
                          </div>
                          {intg.connected ? (
                            <div className="flex items-center gap-2 px-2 py-1 bg-[#ffb0cf]/10 text-[#ffb0cf] rounded-full text-[10px] font-semibold tracking-widest uppercase">
                              <span className="w-1.5 h-1.5 bg-[#ffb0cf] rounded-full animate-pulse"></span>
                              Connected
                            </div>
                          ) : (
                            <div className="flex items-center gap-2 px-2 py-1 bg-[#37333f] text-zinc-500 rounded-full text-[10px] font-semibold tracking-widest uppercase">
                              Disconnected
                            </div>
                          )}
                        </div>
                        <div className="mt-6">
                          <h3 className="text-lg font-semibold text-[#e7dff0] mb-1">{intg.name}</h3>
                          <p className="text-zinc-500 text-xs leading-snug">{intg.desc}</p>
                        </div>
                        <div className="mt-6">
                          <button className="w-full py-2 bg-[#37333f] text-[#e7dff0] text-xs font-medium hover:bg-[#ffb0cf] hover:text-[#3d0023] transition-all rounded-sm">
                            Configure
                          </button>
                        </div>
                      </div>
                    ))}
                    <div className="border-2 border-dashed border-[#574149]/20 p-6 rounded-sm flex flex-col items-center justify-center text-center min-h-[220px] group hover:border-[#ffb0cf]/40 transition-colors duration-300">
                      <div className="w-12 h-12 rounded-full bg-[#2c2834] flex items-center justify-center mb-4 group-hover:bg-[#ffb0cf]/10 transition-colors">
                        <span className="material-symbols-outlined text-zinc-500 group-hover:text-[#ffb0cf]">add</span>
                      </div>
                      <h3 className="text-base font-semibold text-[#e7dff0]">Custom Integration</h3>
                      <p className="text-zinc-600 text-xs mt-1 mb-4">Don&apos;t see what you need?</p>
                      <button className="text-[#ffb0cf] text-xs font-semibold tracking-widest uppercase hover:underline">Request Connector</button>
                    </div>
                  </div>

                  <div className="mt-16">
                    <div className="flex items-center gap-4 mb-8">
                      <h3 className="text-xl font-semibold text-[#e7dff0]">Global Connectivity</h3>
                      <div className="h-px flex-1 bg-[#2c2834]"></div>
                    </div>
                    <div className="grid grid-cols-1 lg:grid-cols-2 gap-12">
                      <div className="space-y-6">
                        {[
                          { icon: 'hub', title: 'Centralized API Keys', desc: 'Manage secret tokens and OAuth credentials for all active integrations from a single vault.' },
                          { icon: 'history', title: 'Sync History', desc: 'View detailed audit logs for every data transmission and connectivity heartbeat.' },
                        ].map(item => (
                          <div key={item.title} className="flex gap-4 p-4 rounded-sm hover:bg-[#1d1a25] transition-colors group">
                            <div className="shrink-0 mt-1"><span className="material-symbols-outlined text-[#ffb0cf]">{item.icon}</span></div>
                            <div>
                              <h4 className="text-[#e7dff0] font-medium">{item.title}</h4>
                              <p className="text-zinc-500 text-xs mt-1">{item.desc}</p>
                            </div>
                          </div>
                        ))}
                      </div>
                      <div className="bg-[#211e29] p-8 rounded-sm relative overflow-hidden">
                        <div className="absolute top-0 right-0 w-32 h-32 bg-[#ffb0cf]/5 rounded-full blur-3xl -mr-16 -mt-16"></div>
                        <h4 className="text-[#e7dff0] font-semibold text-lg mb-4">Enterprise Gateway</h4>
                        <p className="text-zinc-500 text-xs mb-6">Need to connect to on-premise infrastructure? Deploy the AgentShield Gateway to bridge your internal network with our cloud console securely.</p>
                        <div className="flex items-center gap-4">
                          <button className="px-4 py-2 bg-[#37333f] text-[#ffb0cf] text-xs font-semibold tracking-widest uppercase rounded-sm border border-[#ffb0cf]/20 hover:bg-[#ffb0cf] hover:text-[#3d0023] transition-all">
                            Download Gateway
                          </button>
                          <a className="text-zinc-500 text-xs font-medium hover:text-[#e7dff0] flex items-center gap-1 transition-colors" href="#">
                            Documentation <span className="material-symbols-outlined text-xs">open_in_new</span>
                          </a>
                        </div>
                      </div>
                    </div>
                  </div>
                </>
              )}

              {/* Users & RBAC */}
              {tab === 'Users & RBAC' && (
                <>
                  <div className="flex items-end justify-between">
                    <div className="space-y-1">
                      <h3 className="text-3xl font-medium tracking-tighter text-[#e7dff0]">Access Control</h3>
                      <p className="text-zinc-500 text-xs">Manage organizational hierarchy and individual security clearance.</p>
                    </div>
                    <button className="flex items-center gap-2 px-6 py-3 bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] font-semibold text-xs rounded-sm hover:opacity-90 transition-transform active:scale-95 shadow-[0_0_20px_rgba(213,63,140,0.3)]">
                      <span className="material-symbols-outlined text-base">person_add</span>
                      Invite User
                    </button>
                  </div>

                  {/* Stats */}
                  <div className="grid grid-cols-1 md:grid-cols-4 gap-6">
                    {[
                      { label: 'Total Users', value: '124', valueColor: 'text-[#ffb0cf]' },
                      { label: 'Admins', value: '12', valueColor: 'text-[#e7dff0]' },
                      { label: 'Active Now', value: '86', valueColor: 'text-[#e7dff0]', pulse: true },
                      { label: 'Pending Invites', value: '4', valueColor: 'text-[#e7dff0]', border: 'border-l-2 border-[#ffb0cf]/20' },
                    ].map(stat => (
                      <div key={stat.label} className={`bg-[#211e29] p-6 rounded-sm space-y-2 group hover:bg-[#2c2834] transition-colors ${stat.border || ''}`}>
                        <p className="text-xs font-semibold text-zinc-500 uppercase tracking-widest">{stat.label}</p>
                        <div className="flex items-center gap-3">
                          <p className={`text-2xl font-medium ${stat.valueColor}`}>{stat.value}</p>
                          {stat.pulse && <span className="w-2 h-2 rounded-full bg-[#ffb0cf] animate-pulse"></span>}
                        </div>
                      </div>
                    ))}
                  </div>

                  {/* Table */}
                  <div className="bg-[#211e29] overflow-hidden rounded-sm">
                    <div className="px-6 py-4 flex items-center justify-between border-b border-[#574149]/10 bg-[#2c2834]/30">
                      <div className="flex items-center gap-4 bg-[#0f0c17] px-4 py-2 rounded-sm w-96 border border-[#574149]/5">
                        <span className="material-symbols-outlined text-zinc-500">search</span>
                        <input className="bg-transparent border-none focus:ring-0 text-xs text-[#e7dff0] placeholder:text-zinc-600 w-full" placeholder="Search by name, email or role..." type="text" />
                      </div>
                      <button className="p-2 text-zinc-500 hover:text-[#ffb0cf] transition-colors">
                        <span className="material-symbols-outlined">filter_list</span>
                      </button>
                    </div>
                    <table className="w-full text-left border-collapse">
                      <thead>
                        <tr className="text-xs font-semibold text-zinc-500 uppercase tracking-widest">
                          <th className="px-6 py-5 font-medium">User Identity</th>
                          <th className="px-6 py-5 font-medium">Security Role</th>
                          <th className="px-6 py-5 font-medium">Access Status</th>
                          <th className="px-6 py-5 font-medium text-right">Actions</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-[#574149]/5">
                        {users.map((user, i) => (
                          <tr key={i} className="hover:bg-[#2c2834]/50 transition-colors group">
                            <td className="px-6 py-5">
                              <div className="flex items-center gap-4">
                                <div className="w-10 h-10 rounded-sm bg-[#37333f] flex items-center justify-center border border-[#574149]/20 text-[#ffb0cf] font-semibold text-xs">
                                  {user.name.split(' ').map(n => n[0]).join('')}
                                </div>
                                <div>
                                  <p className="text-xs font-medium text-[#e7dff0]">{user.name}</p>
                                  <p className="text-xs text-zinc-500">{user.email}</p>
                                </div>
                              </div>
                            </td>
                            <td className="px-6 py-5">
                              <span className={`px-2 py-1 text-[10px] font-bold uppercase tracking-widest rounded-full ${user.roleClass}`}>{user.role}</span>
                            </td>
                            <td className="px-6 py-5">
                              <div className="flex items-center gap-2">
                                <span className={`w-1.5 h-1.5 rounded-full ${user.statusDot}`}></span>
                                <span className="text-xs font-medium text-zinc-300">{user.status}</span>
                              </div>
                            </td>
                            <td className="px-6 py-5 text-right">
                              <div className="flex items-center justify-end gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                                <button className="p-2 text-zinc-400 hover:text-[#ffb0cf] hover:bg-[#2c2834] rounded-sm transition-all">
                                  <span className="material-symbols-outlined text-base">edit_note</span>
                                </button>
                                <button className="p-2 text-zinc-400 hover:text-[#ffb4ab] hover:bg-[#93000a]/20 rounded-sm transition-all">
                                  <span className="material-symbols-outlined text-base">person_remove</span>
                                </button>
                              </div>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                    <div className="px-6 py-4 flex items-center justify-between border-t border-[#574149]/5">
                      <p className="text-xs text-zinc-500">Showing 4 of 124 users</p>
                      <div className="flex items-center gap-2">
                        <button className="p-1 text-zinc-500 hover:text-[#ffb0cf] disabled:opacity-30" disabled>
                          <span className="material-symbols-outlined">chevron_left</span>
                        </button>
                        {[1, 2, 3].map(p => (
                          <button key={p} className={`w-8 h-8 flex items-center justify-center text-xs font-semibold rounded-sm ${p === 1 ? 'bg-[#ffb0cf] text-[#63003b]' : 'hover:bg-[#2c2834] transition-colors'}`}>{p}</button>
                        ))}
                        <button className="p-1 text-zinc-500 hover:text-[#ffb0cf]">
                          <span className="material-symbols-outlined">chevron_right</span>
                        </button>
                      </div>
                    </div>
                  </div>

                  {/* Role Cards */}
                  <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
                    {rbacCards.map(card => (
                      <div key={card.title} className={`bg-[#1d1a25] p-6 rounded-sm border-t-2 ${card.border}`}>
                        <h4 className="text-xs font-semibold text-[#e7dff0] mb-2 flex items-center gap-2">
                          <span className={`material-symbols-outlined text-xs ${card.iconColor}`}>{card.icon}</span>
                          {card.title}
                        </h4>
                        <p className="text-xs text-zinc-500 leading-relaxed">{card.desc}</p>
                      </div>
                    ))}
                  </div>
                </>
              )}
            </div>
          </div>
        </div>
        </div>
      </main>
    </div>
  )
}
