import type { ReactNode } from 'react'

export function Field({ label, children, className = '' }: { label: string; children: ReactNode; className?: string }) { return <label className={`form-field ${className}`}><span>{label}</span>{children}</label> }
export function Empty({ title, detail }: { title: string; detail: string }) { return <div className="empty-state"><strong>{title}</strong><span>{detail}</span></div> }
export function ErrorBanner({ message }: { message: string }) { return message ? <div className="banner error-banner" role="alert">{message}</div> : null }
export function SuccessBanner({ message }: { message: string }) { return message ? <div className="banner success-banner" role="status">{message}</div> : null }
export function Badge({ children, tone = '' }: { children: ReactNode; tone?: string }) { return <span className={`badge ${tone}`}>{children}</span> }
