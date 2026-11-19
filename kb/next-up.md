# Next up

## 用户确认发布 App 预览与回滚版本后

- 建议发布 `v0.2.0`。上线前在 deploy 仓库核对 tinyhost 是否显式设置 `notify-events` 或 `TINYHOST_NOTIFY_EVENTS`。若设置了自定义列表，与 Owner 核对是否加入 `app.rolled_back` 和 `app.preview_published`。核对结果记入部署仓库的上线记录。[本地实现与验收](sessions/2026-11-19-th-rollback-preview.md)
