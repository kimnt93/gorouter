import { useState } from 'react'
import { Modal } from './Modal'

export function SecretModal({ secret, title, onClose }: { secret: string; title: string; onClose: () => void }) {
  const [copied, setCopied] = useState(false)
  const copy = async () => { await navigator.clipboard.writeText(secret); setCopied(true) }
  return <Modal title={title} onClose={onClose}><div className="secret-warning"><strong>Copy this secret now</strong><span>It cannot be displayed again after this dialog closes.</span></div><code className="secret-value">{secret}</code><div className="dialog-actions"><button className="button" onClick={() => void copy()}>{copied ? 'Copied' : 'Copy secret'}</button><button className="button secondary" onClick={onClose}>I saved it</button></div></Modal>
}
