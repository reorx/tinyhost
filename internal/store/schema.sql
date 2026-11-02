CREATE TABLE clients (
  id            TEXT PRIMARY KEY,           -- "c_" + 随机
  name          TEXT NOT NULL,
  token_hash    TEXT NOT NULL UNIQUE,       -- sha256 hex
  meta          TEXT NOT NULL DEFAULT '{}', -- json：hostname/os/arch/cli_version/request_ip（M2 填写）
  created_at    INTEGER NOT NULL,
  expires_at    INTEGER,                    -- NULL = 永不过期
  revoked_at    INTEGER,
  last_used_at  INTEGER,
  last_used_ip  TEXT
);

CREATE TABLE apps (
  id               TEXT PRIMARY KEY,        -- "a_" + 随机
  slug             TEXT NOT NULL UNIQUE,    -- 含回收站里的；彻底清理时删除整行
  title            TEXT NOT NULL DEFAULT '',
  visibility       TEXT NOT NULL CHECK (visibility IN ('public','private')),
  created_by       TEXT NOT NULL,           -- client id 或 'owner'
  current_version  INTEGER,                 -- NULL = 从未发布
  next_version     INTEGER NOT NULL DEFAULT 1,
  created_at       INTEGER NOT NULL,
  updated_at       INTEGER NOT NULL,
  deleted_at       INTEGER
);

CREATE TABLE layers (
  id          TEXT PRIMARY KEY,             -- "l_" + 随机
  file_count  INTEGER NOT NULL,
  total_size  INTEGER NOT NULL,
  created_at  INTEGER NOT NULL
);
CREATE TABLE layer_files (
  layer_id  TEXT NOT NULL REFERENCES layers(id) ON DELETE CASCADE,
  path      TEXT NOT NULL,                  -- 相对 App 根的完整路径，覆盖层也存完整路径（data/sales.json）
  blob      TEXT NOT NULL,                  -- sha256 hex
  size      INTEGER NOT NULL,
  PRIMARY KEY (layer_id, path)
);
CREATE INDEX layer_files_blob ON layer_files(blob);

CREATE TABLE versions (
  app_id      TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
  number      INTEGER NOT NULL,
  kind        TEXT NOT NULL CHECK (kind IN ('app','data')),
  base_layer  TEXT NOT NULL REFERENCES layers(id),
  manifest    TEXT NOT NULL DEFAULT '{}',   -- App 包里 tinyhost.json 解析后的 title / data
  data_dir    TEXT,                         -- kind=data 时本次推送或撤掉的目录
  message     TEXT NOT NULL DEFAULT '',
  created_by  TEXT NOT NULL,
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (app_id, number)
);
CREATE TABLE version_overlays (
  app_id      TEXT NOT NULL,
  number      INTEGER NOT NULL,
  dir         TEXT NOT NULL,                -- 以 / 结尾，如 data/
  layer       TEXT NOT NULL REFERENCES layers(id),
  pushed_by   TEXT NOT NULL,                -- 这一层最初由谁推送，沿用到后续版本
  pushed_at   INTEGER NOT NULL,
  PRIMARY KEY (app_id, number, dir),
  FOREIGN KEY (app_id, number) REFERENCES versions(app_id, number) ON DELETE CASCADE
);

CREATE TABLE events (
  id      INTEGER PRIMARY KEY,
  at      INTEGER NOT NULL,
  actor   TEXT NOT NULL,                    -- client id 或 'owner'
  action  TEXT NOT NULL,                    -- app.created / app.deployed / data.pushed / data.removed / app.promoted / app.deleted
  app_id  TEXT,
  detail  TEXT NOT NULL DEFAULT '{}'
);
