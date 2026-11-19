CREATE TABLE app_previews (
  app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
  hash TEXT NOT NULL,
  digest TEXT NOT NULL,
  layer TEXT REFERENCES layers(id),
  manifest TEXT NOT NULL DEFAULT '{}',
  message TEXT NOT NULL DEFAULT '',
  created_by TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  published_version INTEGER,
  PRIMARY KEY (app_id, hash)
);
CREATE INDEX app_previews_layer ON app_previews(layer);
ALTER TABLE versions ADD COLUMN rollback_of INTEGER;
ALTER TABLE versions ADD COLUMN preview TEXT;
