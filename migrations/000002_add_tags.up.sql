CREATE TABLE tags (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    slug TEXT NOT NULL UNIQUE,
    color VARCHAR(7),
    created_at TIMESTAMP default NOW(),
    updated_at TIMESTAMP default NOW()
);

CREATE TABLE article_tags (
    article_id UUID REFERENCES articles(id) ON DELETE CASCADE,
    tag_id UUID REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (article_id, tag_id)
);

ALTER TABLE sources
    ADD COLUMN default_tag_id UUID REFERENCES tags(id) ON DELETE SET NULL;

INSERT INTO tags (name, slug, color) VALUES
    ('Tech', 'tech', '#3B82F6'),
    ('Business', 'business', '#10B981'),
    ('Personal', 'personal', '#8B5CF6'),
    ('Politics', 'politics', '#EF4444'),
    ('Economy', 'economy', '#F59E0B'),
    ('Science', 'science', '#06B6D4'),
    ('Health', 'health', '#EC4899'),
    ('Entertainment', 'entertainment', '#F97316');
