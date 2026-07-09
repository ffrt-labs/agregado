DELETE FROM ai_prompts WHERE operation = 'reason';

ALTER TABLE articles DROP COLUMN relevance_reason;
