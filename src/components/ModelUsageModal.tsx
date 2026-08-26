import { useMemo, useState } from 'react'
import type { Price } from '../api/contracts'
import { priceSummary } from '../lib/pricing'
import { Modal } from './Modal'

export function ModelUsageModal({ model, price, onClose }: { model: string; price: Price; onClose: () => void }) {
  const [copied, setCopied] = useState('')
  const baseURL = `${window.location.origin}/v1`
  const commands = useMemo(() => {
    const request = JSON.stringify({ model, messages: [{ role: 'user', content: 'Reply with exactly: connection healthy' }], stream: false })
    return {
      models: [`curl '${baseURL}/models' \\`, '  -H "Authorization: Bearer $GOROUTER_API_KEY"'].join('\n'),
      chat: [`curl '${baseURL}/chat/completions' \\`, "  -H 'Content-Type: application/json' \\", '  -H "Authorization: Bearer $GOROUTER_API_KEY" \\', `  --data-raw '${request}'`].join('\n'),
    }
  }, [baseURL, model])
  const copy = async (name: string, value: string) => { await navigator.clipboard.writeText(value); setCopied(name) }
  return <Modal title={`Use ${model}`} onClose={onClose} className="usage-code-modal">
    <div className="safe-note"><strong>Current login key</strong><span>Set <code>GOROUTER_API_KEY</code> to the API key you used to sign in. Secrets are never read back from the dashboard.</span></div>
    <div className="model-usage-price"><strong>Effective price</strong><span>{priceSummary(price)}</span></div>
    <section className="code-example"><div><strong>List allowed models and prices</strong><button onClick={() => void copy('models', commands.models)}>{copied === 'models' ? 'Copied' : 'Copy'}</button></div><pre>{commands.models}</pre></section>
    <section className="code-example"><div><strong>Send a chat request</strong><button onClick={() => void copy('chat', commands.chat)}>{copied === 'chat' ? 'Copied' : 'Copy'}</button></div><pre>{commands.chat}</pre></section>
  </Modal>
}
