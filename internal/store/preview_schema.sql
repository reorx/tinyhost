CREATE TABLE previews (
  id TEXT PRIMARY KEY,
  token TEXT NOT NULL UNIQUE,
  client_id TEXT NOT NULL,
  local_key TEXT NOT NULL,
  source TEXT NOT NULL DEFAULT '',
  layer TEXT REFERENCES layers(id),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  UNIQUE (client_id, local_key)
);
CREATE INDEX previews_layer ON previews(layer);
