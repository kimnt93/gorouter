import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { getSession } from '../api/client'
import type { Session } from '../api/contracts'

interface SessionValue { session: Session | null; loading: boolean; isMaster: boolean; isMasterView: boolean; viewOrganizationID: string; isViewingAsOrganization: boolean; has: (scope: string) => boolean }
const SessionContext = createContext<SessionValue>({ session: null, loading: true, isMaster: false, isMasterView: false, viewOrganizationID: '', isViewingAsOrganization: false, has: () => false })

export function SessionProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<Session | null>(null)
  const [loading, setLoading] = useState(true)
  useEffect(() => { void getSession().then(setSession).finally(() => setLoading(false)) }, [])
  const viewOrganizationID = new URLSearchParams(window.location.search).get('organization_id') ?? session?.organization_id ?? ''
  const isMaster = session?.role === 'master'
  const value = useMemo<SessionValue>(() => ({ session, loading, isMaster, isMasterView: isMaster && !viewOrganizationID, viewOrganizationID, isViewingAsOrganization: Boolean(viewOrganizationID), has: (scope) => isMaster || session?.scopes.includes(scope) === true }), [session, loading, isMaster, viewOrganizationID])
  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>
}

export const useSession = () => useContext(SessionContext)
