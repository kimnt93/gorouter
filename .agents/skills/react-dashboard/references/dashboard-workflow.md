# React dashboard workflow

## Architecture

- `src/main.tsx` mounts the app.
- `src/App.tsx` chooses the page from the server-owned dashboard path.
- `src/components/AppShell.tsx` owns navigation and context controls.
- `src/context/SessionContext.tsx` loads the safe session and exposes principal,
  scopes, memberships, and organization View As state.
- `src/api/contracts.ts` and `src/api/client.ts` are the typed browser boundary.
- `src/pages/` composes shared components; `src/styles/app.css` is the design
  system.
- Vite writes committed assets to `internal/api/spa/dist`, embedded by Go.

Do not add a client router dependency merely to dispatch the current finite
dashboard paths. Do not call APIs from arbitrary components when a page hook or
client function can own the request.

## Change sequence

1. Inspect the server response type and route first.
2. Update `contracts.ts` and a focused `client.ts` function.
3. Decide whether behavior belongs in a shared component/hook or one page.
4. Handle loading, empty, error, success, retry, and permission states.
5. Preserve the current `organization_id` View As query only for endpoints that
   accept organization narrowing; do not attach it to personal-only calls.
6. Add Testing Library tests for state transitions and interactions.
7. Run `npm test -- --run`, then `npm run build` to refresh the embedded assets.
8. Use the desktop browser smoke for layout, dialog, navigation, and real API
   wiring changes.

## Component conventions

- Use semantic buttons, labels, tables, lists, forms, and dialogs.
- Use `Modal`/`SecretModal` for bounded dialogs and one-time secret handling.
- Use `SearchableSelect` for non-trivial option lists. Give every select and
  dropdown styled trigger/options, keyboard navigation, search, clear state,
  and visible selection.
- Reuse shared range and usage filters so Analysis, Logs, and Cache encode
  user/API-key/organization selections identically.
- Number table/list rows when users need stable visual order.
- Keep async cancellation/abort handling for streaming provider chat tests.

## Canonical React examples

Keep the browser contract and request function typed. Encode path values and
use `URLSearchParams` for query values instead of string concatenation.

```ts
// src/api/contracts.ts
export interface Widget {
  id: string
  name: string
  status: 'active' | 'disabled'
}

// src/api/client.ts
export const getWidgets = (query = ''): Promise<ListResponse<Widget>> => {
  const params = new URLSearchParams({ limit: '100' })
  if (query.trim()) params.set('q', query.trim())
  return request<ListResponse<Widget>>(`/admin/widgets?${params}`).then(normalizeList)
}
```

Compose existing controls and presentation helpers. Keep authorization/context
in the page/session layer, use component classes rather than repeated inline
styles, number operational rows, and truncate only safe display values.

```tsx
interface WidgetTableProps {
  widgets: Widget[]
  selected: string
  onSelect: (id: string) => void
}

export function WidgetTable({ widgets, selected, onSelect }: WidgetTableProps) {
  const options = widgets.map((widget) => ({
    value: widget.id,
    label: widget.name,
    meta: widget.status,
  }))

  return <section className="panel">
    <SearchableSelect
      value={selected}
      options={options}
      onChange={onSelect}
      placeholder="Select a widget"
      searchPlaceholder="Search widgets"
    />
    <table className="data-table">
      <thead><tr><th>#</th><th>Name</th><th>Status</th></tr></thead>
      <tbody>{widgets.map((widget, index) => <tr key={widget.id}>
        <td>{index + 1}</td>
        <td><TruncatedText>{widget.name}</TruncatedText></td>
        <td><span className={`status-pill ${widget.status}`}>{widget.status}</span></td>
      </tr>)}</tbody>
    </table>
  </section>
}
```

Test observable behavior and accessible roles rather than component internals:

```tsx
test('selects a searchable widget option', async () => {
  const user = userEvent.setup()
  const onSelect = vi.fn()
  render(<WidgetTable widgets={fixtures} selected="" onSelect={onSelect} />)

  await user.click(screen.getByRole('button', { name: /select a widget/i }))
  await user.type(screen.getByRole('textbox', { name: /search widgets/i }), 'alpha')
  await user.click(screen.getByRole('option', { name: /alpha/i }))

  expect(onSelect).toHaveBeenCalledWith('widget_alpha')
})
```

## CSS

- Use existing custom properties, typography scale, spacing, status pills,
  buttons, panels, and table primitives in `src/styles/app.css`.
- Prefer component classes over inline style objects for repeated behavior.
- Keep the current larger readable font scale and compact operational density.
- Keep activity/chart bars visually compact; data volume should not make a bar
  dominate the page.
- Reuse the vertical chart interaction contract: buckets stay chronological,
  sparse series anchor to the right edge, and columns flex to fit the available
  panel width without horizontal overflow.
- Make the whole time-bucket column the pointer/focus target, not only the
  painted bar. Position the tooltip above the pointer with
  `pointer-events: none` so it does not block movement to adjacent bars.
- Constrain tables/dialogs inside the viewport. Desktop is the requested
  priority; maintain basic overflow safety without an unsolicited mobile
  redesign.
- Long IDs, model names, labels, prompts-safe metadata, and errors must not
  stretch layouts. Render a shortened value and expose the complete *safe*
  value through an accessible tooltip/title or details view.
