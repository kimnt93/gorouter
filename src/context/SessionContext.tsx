import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { getSession } from '../api/client'
import type { Session } from '../api/contracts'

interface SessionValue { session: Session | null; loading: boolean; isMaster: boolean; has: (scope: string) => boolean }
const SessionContext = createContext<SessionValue>({ session: null, loading: true, isMaster: false, has: () => false })

export function SessionProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<Session | null>(null)
  const [loading, setLoading] = useState(true)
  useEffect(() => { void getSession().then(setSession).finally(() => setLoading(false)) }, [])
  const value = useMemo<SessionValue>(() => ({ session, loading, isMaster: session?.role === 'master', has: (scope) => session?.role === 'master' || session?.scopes.includes(scope) === true }), [session, loading])
  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>
}

export const useSession = () => useContext(SessionContext)
