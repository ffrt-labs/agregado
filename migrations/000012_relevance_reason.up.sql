ALTER TABLE articles ADD COLUMN relevance_reason TEXT;

INSERT INTO ai_prompts (operation, system_prompt) VALUES
    ('reason', 'You are a news analyst. Given an article title and content, explain in one short sentence (max 20 words) why this article matters to a curious reader. Return only that sentence — no preamble, no quotes, no explanation of your reasoning.');
