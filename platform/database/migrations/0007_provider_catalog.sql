ALTER TABLE credentials DROP CONSTRAINT IF EXISTS credentials_provider_valid;
ALTER TABLE credentials ADD CONSTRAINT credentials_provider_valid CHECK (
    provider IN (
        'claude', 'codex', 'openai', 'anthropic', 'gemini', 'groq',
        'openrouter', 'opencode-zen', 'xai', 'deepseek', 'moonshot',
        'qwen', 'openai-compatible'
    )
) NOT VALID;
