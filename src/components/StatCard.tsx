import type { ReactNode } from 'react'

export function StatCard({ label, value, detail, accent }: { label: string; value: ReactNode; detail: string; accent?: string }) {
  return <article className="stat-card"><div className={`stat-accent ${accent ?? ''}`} /><span>{label}</span><strong>{value}</strong><small>{detail}</small></article>
}
