# Domain invariants

## Principals and context

| Principal | Base visibility | Organization context |
|---|---|---|
| Master | Global | May View As one organization admin; can always return to master |
| User | Own private data, own usage, and organizations where they are an active member | May create organizations; the creator becomes that organization’s initial membership admin. May View As only organizations they administer; selecting context never grants membership or an admin role. |
| Organization key | Its organization and stored scopes | Fixed to its owner organization |

Authentication resolves current status every time it matters: enabled key,
active owner, active organization, active membership where required, immutable
owner/context shape, scopes, and allowed models. A signed cookie does not make
stale authorization true.

## View As

View As is presentation/query context, similar to inspecting the system through
an organization's admin lens:

- Master sees a master option plus every permitted organization.
- Organization admins see only organizations they administer.
- Ordinary members/users do not see master and do not gain admin visibility.
- Selecting an organization filters pages, navigation, model/key choices, and
  queries to that organization.
- It does not rewrite API keys, credentials, memberships, actor snapshots, or
  billing ownership.
- The server validates every requested context; a URL query value is not proof
  of authority.

## Ownership and privacy

- A key is owned by exactly one user or organization. Organization-owned keys
  have the same organization context. User-owned keys may be personal or scoped
  to an active membership.
- Key ownership/context are immutable; rotation changes only the secret.
- An organization admin can manage organization resources and create selected-
  model keys for organization members according to policy.
- A member's personal credentials/OAuth and personal usage remain private from
  the organization admin.
- New API-key plaintext is returned once and may be retained only encrypted at
  rest for the authorized reveal flow. Lists expose only safe metadata. Legacy
  keys without encrypted reveal material require rotation before reveal.

## Model and routing context

```text
personal/global:       <provider-prefix>/<upstream-model>
organization-owned:   <organization-slug>/<provider-prefix>/<upstream-model>
```

- Use `pkg/provider` helpers, not string concatenation.
- Personal keys can use eligible personal/global routes without organization
  attribution.
- Organization-context calls can use routes legal for that organization.
- A blend is a stack of routes behind one public model; route selection retains
  the original credential/upstream model for pricing and usage. User-entered
  blend names accept only letters, digits, `_`, and `-`; provider/system
  namespaces are generated server-side.
- `auto`, `<provider-prefix>/auto`, and `<blend>/auto` are real callable aliases.
  They randomize only eligible routes, obey `AUTO_MAX_TRIES`, preserve route
  ownership/quota/health, and record the selected upstream model. Provider
  discovery/list selectors include `auto`.
- Auto price presentation is the arithmetic average of available routed prices,
  rounded to four decimals; actual usage cost uses the selected route’s original
  model price.
- Model allowlists fail closed. `/v1/models` returns exactly enabled models the
  key may call, including safe price information where the contract provides
  it.

## Usage and billing

- Master actor is `master`; organization-key actor is `org:<name>`; user actor
  stores the user ID/name snapshot; legacy remains explicitly legacy.
- Organization attribution comes from the request/key/route context, never from
  a user's mere membership.
- Store input, output, cache read, and cache write token/cost components.
- Missing price resolves to Free/zero and remains priced/settled, avoiding an
  ambiguous unpriced state.
- Durable usage visibility: master all; user own actor; organization admin/key
  matching organization; organization member own actor only.

## Audit and sensitive data

Audit administrative mutations separately from model usage. Store safe actor,
organization, action, target, and non-secret changed fields only. Never include
keys/hashes, credentials/OAuth, cookies/headers, prompts/completions, or raw
provider errors.
