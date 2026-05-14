import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../api/client'

const sqlNotifyEvents = `
SELECT detected_at, rule_id, rule_name, severity, agent_id, deployment_id, session_id, hook_name, message
FROM agentguard_notify_events
SETTINGS seek_to='-24h'`

export function useLiveThreatFeed(limit = 20, enabled = true) {
  const [threats, setThreats] = useState([])
  const [connected, setConnected] = useState(false)
  const [error, setError] = useState(null)
  const [ruleCategory, setRuleCategory] = useState({})

  useEffect(() => {
    if (!enabled) return

    api.listRules()
      .then(d => {
        const map = {}
        for (const r of (d.rule_packs || [])) map[r.id] = r.category
        setRuleCategory(map)
      })
      .catch(() => {})
  }, [enabled])

  useEffect(() => {
    if (!enabled) return

    const ctrl = new AbortController()

    async function stream() {
      let first = true
      setError(null)
      for await (const row of api.protonQuery(sqlNotifyEvents, { signal: ctrl.signal })) {
        if (first) {
          setConnected(true)
          first = false
        }
        setThreats(prev => {
          const key = r => `${r.deployment_id}|${r.agent_id}|${r.session_id}|${r.rule_id}`
          const next = [row, ...prev.filter(r => key(r) !== key(row))]
          return next
            .sort((a, b) => new Date(b.detected_at) - new Date(a.detected_at))
            .slice(0, limit)
        })
      }
      setConnected(false)
    }

    stream().catch(e => {
      if (e?.name !== 'AbortError') {
        setConnected(false)
        setError(e.message || 'Connection failed')
      }
    })

    return () => {
      ctrl.abort()
      setConnected(false)
    }
  }, [enabled, limit])

  return { threats, connected, error, ruleCategory }
}

export function useThreatNavigation() {
  const navigate = useNavigate()

  return useCallback(async (threat) => {
    try {
      const data = await api.getThreats({ agent_id: threat.agent_id })
      const match = (data.threats || []).find(th =>
        th.rule_id === threat.rule_id && th.session_id === threat.session_id
      )
      navigate(match ? `/threats/${match.threat_id}` : '/threats')
    } catch {
      navigate('/threats')
    }
  }, [navigate])
}

export function useOpenThreats(enabled = true) {
  const [threats, setThreats] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  useEffect(() => {
    if (!enabled) return

    let cancelled = false

    async function fetchOpenThreats() {
      try {
        const limit = 200
        let page = 1
        let total = 0
        const next = []

        do {
          const data = await api.getThreats({ status: 'open', page, limit })
          next.push(...(data.threats || []))
          total = data.total || next.length
          page += 1
        } while (next.length < total)

        if (!cancelled) {
          setThreats(next)
          setError(null)
        }
      } catch (err) {
        if (!cancelled) setError(err?.message || 'Failed to load threats')
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    fetchOpenThreats()
    const timer = setInterval(fetchOpenThreats, 30_000)

    return () => {
      cancelled = true
      clearInterval(timer)
    }
  }, [enabled])

  return { threats, loading, error }
}
