# Quota, Cost, and Usage Specification

## Price Records

Prices are per one million tokens:

- Input
- Output
- Cache read
- Cache write

Prices may be set manually and later synchronized from OpenRouter or LiteLLM. External synchronization must not block request serving. See https://openrouter.ai/api/frontend/v1/catalog/models


## Cost

```text
cost = input_tokens * input_price / 1e6
      + output_tokens * output_price / 1e6
      + cache_read_tokens * cache_read_price / 1e6
      + cache_write_tokens * cache_write_price / 1e6
```

Missing price means unpriced usage; do not invent a price.

## Monthly Quota

Before forwarding, estimate input and output cost. The estimate uses request content and bounded requested output tokens.

For multi-node correctness:

- Reserve budget atomically in Redis.
- Reject with HTTP 429 when the reservation exceeds the monthly limit.
- Settle the reservation using actual usage after completion.
- Release unused reservation.

Do not rely only on a PostgreSQL `SUM()` under concurrent load.

## RPM

Optional API-key requests-per-minute limits use Redis atomic counters with expiration. A Redis outage must follow an explicit configured policy; strict mode fails closed.

## Usage Events

Record timestamp, tenant, API key, credential, client model, upstream model, input/output/cache tokens, cost, cache-hit flag, status, duration, and safe error summary.

Usage writes should be buffered and batched. PostgreSQL is the initial durable store. A future ClickHouse sink must implement the same usage repository boundary.
