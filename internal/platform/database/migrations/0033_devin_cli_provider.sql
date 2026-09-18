-- Devin CLI uses an apk_user key with the local ACP binary, not the
-- Devin Desktop GetUserJwt protocol. Keep provider catalog parity tested.
ALTER TABLE credentials DROP CONSTRAINT IF EXISTS credentials_provider_valid;
ALTER TABLE credentials ADD CONSTRAINT credentials_provider_valid CHECK (
    provider IN (
        'claude', 'codex', 'github-copilot', 'cursor', 'grok-build',
        'xai-oauth', 'kimi-code', 'cline', 'clinepass', 'kilo-code',
        'kiro', 'amazon-q', 'antigravity', 'devin-desktop', 'devin-cli',
        'openai', 'anthropic', 'gemini', 'groq', 'openrouter',
        'opencode-zen', 'opencode-go', 'xai', 'deepseek', 'moonshot',
        'qwen', 'openai-compatible'
    )
) NOT VALID;
