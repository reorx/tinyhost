CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE device_requests (
 id TEXT PRIMARY KEY, device_code_hash TEXT NOT NULL UNIQUE, user_code TEXT NOT NULL UNIQUE,
 name TEXT NOT NULL, meta TEXT NOT NULL, status TEXT NOT NULL CHECK(status IN ('pending','approved','denied','consumed')),
 approved_name TEXT, token_ttl INTEGER, client_id TEXT, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, last_polled_at INTEGER
);
CREATE TABLE sessions (
 id_hash TEXT PRIMARY KEY, kind TEXT NOT NULL CHECK(kind IN ('admin','viewer')), created_at INTEGER NOT NULL,
 expires_at INTEGER NOT NULL, ip TEXT, user_agent TEXT
);
CREATE TABLE view_tickets (ticket_hash TEXT PRIMARY KEY, next_path TEXT NOT NULL, expires_at INTEGER NOT NULL, used_at INTEGER);
CREATE TABLE slug_redirects (old_slug TEXT PRIMARY KEY, app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE, created_at INTEGER NOT NULL);
CREATE TRIGGER reserve_redirected_slugs BEFORE INSERT ON apps
WHEN EXISTS(SELECT 1 FROM slug_redirects WHERE old_slug=NEW.slug)
BEGIN SELECT RAISE(ABORT, 'slug is reserved by a redirect'); END;
