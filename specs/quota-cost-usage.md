# Quota, Cost, and Usage Specification

## Price Records

Prices are per one million tokens:

- Input
- Output
- Cache read
- Cache write

Manual prices override synchronized catalog prices. The runtime synchronizes the OpenRouter frontend catalog immediately at startup and then once per hour by default. External synchronization must not block request serving or replace the last usable in-memory snapshot when a fetch fails. See <https://openrouter.ai/api/frontend/v1/catalog/models>.

The catalog store is intentionally compact: one canonical model row containing the model/name/provider metadata, context length, cache capability, four per-million rates, source, and update time. Do not persist the upstream response or duplicate cached/non-cached estimate totals. When OpenRouter publishes multiple endpoint variants, select the standard variant deterministically.

The resolver uses an atomically replaced in-memory snapshot. Resolution order is:

1. Manual price for the public model.
2. Catalog price for the public model.
3. Manual price for the upstream model.
4. Catalog price for the upstream model.


## Cost

```text
cost = input_tokens * input_price / 1e6
      + output_tokens * output_price / 1e6
      + cache_read_tokens * cache_read_price / 1e6
      + cache_write_tokens * cache_write_price / 1e6
```

A model missing from manual and catalog pricing is explicitly treated as Free:
all four rates and the recorded cost are zero, and the usage event remains
priced so quota and reporting never carry an ambiguous unpriced state.

`GET /admin/pricing/catalog` exposes search (`q`) and pagination (`limit`, `offset`). `GET /admin/pricing/estimate` accepts `model` or `upstream_model`, plus non-negative `prompt_tokens` and `completion_tokens`, and returns rates with derived `without_cache` and `with_cache` costs. If cache pricing is unavailable, the cached estimate uses the regular input rate; it must not imply free cache reads.

## Quota Periods

Before forwarding, estimate input and output cost. The estimate uses request content and bounded requested output tokens.

An API key selects one quota mode:

- `none`: no USD quota; no reservation is made.
- `day`: UTC calendar day.
- `week`: ISO week beginning Monday at 00:00 UTC.
- `month`: UTC calendar month.

The legacy `monthly_quota_usd` API/database field remains compatible and maps to `quota_usd` with `quota_period=month`. New callers should use `quota_usd` and `quota_period`.

For multi-node correctness:

- Reserve budget atomically in Redis.
- Combine durable usage since the current window began with in-process pending usage.
- Reject with HTTP 429 when the reservation exceeds the selected window limit.
- Settle the reservation using actual usage after completion.
- Release unused reservation.
- Expire Redis reservation state at the end of the selected window.

Do not rely only on a PostgreSQL `SUM()` under concurrent load.

## RPM

Optional API-key requests-per-minute limits use Redis atomic counters with expiration. A Redis outage must follow an explicit configured policy; strict mode fails closed.

## Usage Events

Record timestamp, tenant, API key, credential, client model, upstream model, input/output/cache tokens, cost, cache-hit flag, status, duration, and safe error summary.

Usage writes should be buffered and batched. PostgreSQL is the initial durable store. A future ClickHouse sink must implement the same usage repository boundary.

## Dashboard Reporting Requirements

Analysis, request logs, and provider-cache reporting share one filter model:

- Range: `1D`, `7D`, `30D`, `90D`, `YTD`, `All`, or a custom range.
- Resolution is rendered as three visible controls: `Hour`, `Day`, and `Week`.
- One filter dimension is selected at a time: user, API key, or organization.
- The selected dimension accepts multiple values and displays every currently
  selected value. API requests encode repeated IDs as comma-separated query
  values.
- Log-specific model and HTTP-status filters remain available after the shared
  identity filter.

Analysis uses vertical charts:

- Requests are grouped by time bucket and stacked by user. Each bar represents
  100% of requests in that bucket; the tooltip shows absolute request counts.
- Token and cost bars use absolute scale. Token tooltips show input, output,
  cache-read, and cache-write tokens. Cost tooltips show the same four cost
  components.
- The model breakdown reports model, request count, input, output, cache read,
  cache write, total tokens, cost, and percentage of selected token volume.

The cache page reuses the same filter and vertical-chart language. Cache-read
share is bounded to 100% and is calculated as cache-read tokens divided by all
input-context tokens (`input + cache read`). Both percentage and token totals
must be displayed.

## Provider Ownership and Quota State

Provider credentials are personal to the user who connects them. The provider
connection form does not expose an owner-organization selector. The server
derives ownership from the authenticated principal; master-created credentials
remain globally available for operational compatibility.

Manual provider quota reloads persist the latest safe snapshot. The dashboard
reads the persisted snapshot without contacting the provider and only performs
a remote refresh after the user selects **Reload**. Quota-provider routing is
fill-first in configured route order. The currently selected account is exposed
to the dashboard and visibly highlighted; exhausted accounts move out of the
active position until their reset time.
