---
name: swaggo-docs
description: Add or review GoRouter Swag annotations, regenerate committed Swagger docs with Swaggo, verify API contract coverage, or diagnose generated-document drift and parser errors. Use when handler routes, request/response types, statuses, security, or internal/docs change. Do not use for prose-only documentation.
---

# Swaggo documentation

Read [swaggo-workflow.md](references/swaggo-workflow.md) before changing
annotations or diagnosing generation. Follow its canonical annotation example.
Use `scripts/generate.sh check` to detect drift and `scripts/generate.sh
generate` after the source contract is correct.

## Contract rules

- Document the real Fiber route and typed request/response structs; never patch
  generated output to conceal a missing annotation or unsupported type.
- Include summary, tags, security, parameters, success status/type, and every
  meaningful error status using `responseapi.ErrorResponse`.
- Keep the operation's object-level authorization in implementation/tests;
  Swagger security metadata does not enforce policy.
- Regenerate `internal/docs/docs.go`, `swagger.json`, and `swagger.yaml`
  together. Review route/type changes before accepting the diff.
- Run Swagger, named-contract, and response-contract tests plus
  `git diff --check`.
