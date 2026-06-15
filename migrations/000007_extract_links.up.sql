ALTER TABLE articles ADD COLUMN parent_article_id UUID REFERENCES articles(id);

ALTER TABLE sources ADD COLUMN extract_links BOOLEAN NOT NULL DEFAULT true;
