-- The service catalog is authoritative for validation. Keep this database
-- allowlist in sync with pkg/provider/catalog.go; see the catalog parity test.
-- Dropping the stale constraint first permits existing installations to use
-- recently added providers without rewriting historical credential rows.
ALTER TABLE credentials DROP CONSTRAINT IF EXISTS credentials_provider_valid;
ALTER TABLE credentials ADD CONSTRAINT credentials_provider_valid CHECK (
    provider IN (
        'claude', 'codex', 'github-copilot', 'cursor', 'grok-build',
        'xai-oauth', 'kimi-code', 'cline', 'clinepass', 'kilo-code',
        'kiro', 'amazon-q', 'antigravity', 'devin-desktop',
        'openai', 'anthropic', 'gemini', 'groq', 'openrouter',
        'opencode-zen', 'opencode-go', 'xai', 'deepseek', 'moonshot',
        'qwen', 'openai-compatible'
    )
) NOT VALID;
