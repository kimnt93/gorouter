# User-scoped usage contract (v0.2.1)

This replaces the earlier workload-key draft. A user has one canonical API key.
Agent IDs are per-request correlation underneath authenticated user ownership.

- [User model access, aliases, groups and organization limits](user-model-access.md)
- [Tracking headers and multi-select usage REST APIs](usage-tracking.md)

The existing `/admin/usage/workloads/weekly` path remains, with user/org
visibility and optional multi-agent filters. Capability: `gorouter-user-usage-v1`.
No independently editable agent-budget policy or per-agent authentication key is
introduced. Ordinary usage logs retain their documented durability/measurement
limitations; organization budget admission uses a separate durable reservation
record in the same selected backend.
