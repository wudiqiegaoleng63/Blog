-- 0001_init.down.sql
-- 回滚 0001_init.up.sql。仅用于开发环境演练，生产慎用。

DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS background_jobs;
DROP TABLE IF EXISTS comments;
DROP TABLE IF EXISTS post_tags;
DROP TABLE IF EXISTS post_categories;
DROP TABLE IF EXISTS posts;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS categories;
DROP TABLE IF EXISTS user_action_tokens;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS user_profiles;
DROP TABLE IF EXISTS users;
