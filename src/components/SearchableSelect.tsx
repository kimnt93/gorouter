import { useEffect, useMemo, useRef, useState } from 'react'

export interface SelectOption {
  value: string
  label: string
  meta?: string
}

interface Props {
  value: string
  options: SelectOption[]
  onChange: (value: string) => void
  placeholder?: string
  searchPlaceholder?: string
  disabled?: boolean
  className?: string
}

export function SearchableSelect({ value, options, onChange, placeholder = 'Select an option', searchPlaceholder = 'Search options', disabled = false, className = '' }: Props) {
  const root = useRef<HTMLDivElement>(null)
  const searchInput = useRef<HTMLInputElement>(null)
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const selected = options.find((option) => option.value === value)
  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase()
    return needle ? options.filter((option) => `${option.label} ${option.meta ?? ''}`.toLowerCase().includes(needle)) : options
  }, [options, query])

  useEffect(() => {
    if (!open) return
    const close = (event: MouseEvent) => { if (!root.current?.contains(event.target as Node)) setOpen(false) }
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') setOpen(false) }
    document.addEventListener('mousedown', close)
    document.addEventListener('keydown', escape)
    requestAnimationFrame(() => searchInput.current?.focus())
    return () => { document.removeEventListener('mousedown', close); document.removeEventListener('keydown', escape) }
  }, [open])

  const choose = (next: string) => { onChange(next); setOpen(false); setQuery('') }
  return <div className={`searchable-select ${open ? 'open' : ''} ${className}`.trim()} ref={root}>
    <button type="button" className="searchable-select-trigger" disabled={disabled} aria-haspopup="listbox" aria-expanded={open} title={selected?.label ?? placeholder} onClick={() => setOpen((current) => !current)}>
      <span>{selected?.label ?? placeholder}</span><i aria-hidden="true">⌄</i>
    </button>
    {open && <div className="searchable-select-menu">
      <div className="select-search"><span aria-hidden="true">⌕</span><input ref={searchInput} value={query} onChange={(event) => setQuery(event.target.value)} placeholder={searchPlaceholder} aria-label={searchPlaceholder} /></div>
      <div className="searchable-select-list" role="listbox">
        {filtered.length === 0 ? <div className="select-empty">No matching options</div> : filtered.map((option) => {
          const number = options.findIndex((candidate) => candidate.value === option.value) + 1
          return <button type="button" role="option" aria-selected={option.value === value} className={option.value === value ? 'selected' : ''} key={option.value} title={[option.label, option.meta].filter(Boolean).join(' · ')} onClick={() => choose(option.value)}>
            <b>{String(number).padStart(2, '0')}</b><span><strong>{option.label}</strong>{option.meta && <small>{option.meta}</small>}</span><i aria-hidden="true">{option.value === value ? '✓' : ''}</i>
          </button>
        })}
      </div>
      <div className="select-count">{filtered.length} of {options.length} options</div>
    </div>}
  </div>
}

export function TruncatedText({ children, className = '' }: { children: string; className?: string }) {
  return <span className={`truncate-hover ${className}`.trim()} title={children}>{children}</span>
}
