---
name: react-dashboard
description: Add, update, review, or diagnose GoRouter's Vite React TypeScript operations dashboard, typed browser contracts, reusable controls, filters, tables, dialogs, charts, CSS, View As behavior, and embedded SPA build. Use for files under src or dashboard presentation. Do not follow superseded specs that prohibit React.
---

# React dashboard

Read [dashboard-workflow.md](references/dashboard-workflow.md) before editing the
SPA. Read [ui-contracts.md](references/ui-contracts.md) for current visibility,
formatting, and interaction rules.

## Workflow

1. Reuse typed contracts from `src/api/contracts.ts` and request helpers from
   `src/api/client.ts`. Keep server and browser field names aligned.
2. Reuse or extend shared components before adding page-local variants,
   especially `Modal`, `SearchableSelect`, range/usage filters, token
   breakdowns, charts, and management layout primitives.
3. Get principal, scopes, memberships, and View As context from
   `SessionContext`. Hide unauthorized actions for clarity, but rely on the
   server for enforcement.
4. Preserve organization context in relevant navigation and API filters. Only
   master may select master context; ordinary users never receive that option.
5. Keep operational tables readable on desktop. Truncate long values in the
   layout and expose the complete safe value with native title/tooltip or an
   accessible detail view. Never put secrets, prompts, completions, hashes, or
   cookies into browser state.
6. Style every select/dropdown consistently, make long lists searchable, and
   number list/table rows where order matters. Keep charts compact and labels
   legible.
7. Add Vitest/Testing Library coverage for behavior and a desktop browser smoke
   assertion for layout-critical changes.
8. Run `npm test -- --run` and `npm run build`. Commit the generated
   `internal/api/spa/dist` output; do not edit it manually.

The current product targets desktop/PC presentation first. Maintain reasonable
overflow safety, but do not redesign for mobile unless the request includes it.
