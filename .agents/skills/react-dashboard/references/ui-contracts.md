# Current dashboard contracts

## Navigation and View As

- Master sees global management and a context selector with **Master** plus
  organizations. Selecting an organization makes organization-sensitive pages
  behave as that organization's admin view; master can always switch back.
- Organization admins see organization context only for organizations they
  administer. Ordinary users/members never see or select Master.
- Context affects only authorized organization data. The server still validates
  every request and the browser keeps `organization_id` in relevant links and
  filters.
- Organization listings show total member counts.

## Lists, selects, and long text

- Style every native/custom select and dropdown consistently.
- Add search when the list can be non-trivial: users, API keys, organizations,
  models, routes, credentials, and provider-discovered models.
- Show visible selected values and clear/reset behavior.
- Number rows/options where ordinal orientation helps.
- Shorten long display text to protect layouts; hover/focus or an explicit
  details action reveals the complete safe value. Never reveal a secret merely
  because the pointer hovers.

## Usage and logs

- Shared range controls: `1D`, `7D`, `30D`, `90D`, `YTD`, `All`, and custom.
- Shared grouping controls: Hour, Day, Week.
- Shared multi-select dimension: user, API key, or organization, constrained by
  current visibility.
- Token order is `[input / output / cache read / cache write]`.
- Log rows show zero cost for Free models. Detail dialogs show safe request
  metadata and conversation availability boundaries; prompts/completions are
  not captured or displayed unless a future explicit secure contract changes
  that policy.
- Provider cache page reports upstream cache-read/cache-write tokens separately
  from router Redis response-cache statistics.

## Models, routes, keys, and providers

- Model pages list all models available through all visible providers and show
  input/output/cache-read/cache-write rates next to the model. Round displayed
  rates to at most four decimal places.
- Missing price is labeled Free and displayed as zero cost.
- Model route editors support a stack of routes behind one public model.
- Model/key usage help includes a copyable curl example that uses the current
  login/API-key context without serializing secrets into persisted browser
  state.
- API-key creation assigns a key to one user in the selected organization (or
  to the organization where policy allows), selects allowed models, and applies
  quota/RPM. Chat is the essential API-key scope; do not expose obsolete scope
  controls unless policy requires management keys.
- The allowed-model list is the full set the actor may grant in the current
  context, not a hard-coded smoke subset.
- Provider connection screens support API-key/OAuth connection, health,
  discovery/import, bounded chat test, quota reload, enable/disable, and delete.
  Personal connections remain invisible to organization admins.
