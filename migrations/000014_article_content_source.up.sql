ALTER TABLE articles ADD COLUMN distilled_content TEXT;
ALTER TABLE articles ADD COLUMN content_source VARCHAR(32)
	CHECK (content_source IN ('fetched', 'feed_content', 'feed_description', 'newsletter'));
