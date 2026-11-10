-- Langfuse owns prompts and model-call history (ADR 0011, #207). The in-code
-- defaults remain the fallback; the `ai: usage` log line is unaffected.
DELETE FROM admin_settings WHERE key = 'ai_logging_enabled';
DROP TABLE IF EXISTS ai_logs;
DROP TABLE IF EXISTS ai_prompts;
