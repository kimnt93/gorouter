-- Devin Cloud uses current cog_ PAT/service-user credentials against the v3
-- asynchronous agent-session API. It remains distinct from CLI and Desktop.
ALTER TABLE credentials DROP CONSTRAINT IF EXISTS credentials_provider_valid;
ALTER TABLE credentials ADD CONSTRAINT credentials_provider_valid CHECK (
    provider IN (
        'claude', 'codex', 'github-copilot', 'cursor', 'grok-build',
        'xai-oauth', 'kimi-code', 'cline', 'clinepass', 'kilo-code',
        'kiro', 'amazon-q', 'antigravity', 'devin', 'devin-desktop', 'devin-cli',
        'openai', 'anthropic', 'gemini', 'groq', 'openrouter',
        'opencode-zen', 'opencode-go', 'xai', 'deepseek', 'moonshot',
        'qwen', 'openai-compatible'
    )
) NOT VALID;
