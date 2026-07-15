-- 0001_init.up.sql
-- 初始化博客核心表结构。
--
-- 约定：
--   - 内部主键 BIGINT UNSIGNED AUTO_INCREMENT。
--   - 对外标识 public_id CHAR(26) ULID，全局唯一。
--   - 时间统一 UTC DATETIME(6)。
--   - 字符集 utf8mb4，排序规则 utf8mb4_0900_ai_ci。
--
-- 本文件只包含 MVP 阶段必需的表；post_revisions、outbox_events、
-- ai_documents 等在生产加固或 AI 阶段再通过后续 migration 增加。

CREATE TABLE users (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id       CHAR(26)        NOT NULL,
    email           VARCHAR(254)    NOT NULL,
    email_normalized VARCHAR(254)   NOT NULL,
    username        VARCHAR(32)     NOT NULL,
    password_hash   VARCHAR(255)    NOT NULL,
    role            VARCHAR(16)     NOT NULL DEFAULT 'user',
    status          VARCHAR(16)     NOT NULL DEFAULT 'active',
    email_verified_at DATETIME(6)   NULL,
    token_version   BIGINT UNSIGNED NOT NULL DEFAULT 1,
    last_login_at   DATETIME(6)     NULL,
    created_at      DATETIME(6)     NOT NULL,
    updated_at      DATETIME(6)     NOT NULL,
    deleted_at      DATETIME(6)     NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_users_public_id (public_id),
    UNIQUE KEY uk_users_email_normalized (email_normalized),
    UNIQUE KEY uk_users_username (username),
    KEY idx_users_status (status),
    KEY idx_users_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE user_profiles (
    user_id         BIGINT UNSIGNED NOT NULL,
    display_name    VARCHAR(64)     NOT NULL,
    bio             VARCHAR(500)    NULL,
    avatar_url      VARCHAR(512)    NULL,
    website_url     VARCHAR(512)    NULL,
    location        VARCHAR(128)    NULL,
    created_at      DATETIME(6)     NOT NULL,
    updated_at      DATETIME(6)     NOT NULL,
    PRIMARY KEY (user_id),
    CONSTRAINT fk_profiles_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE refresh_tokens (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id       CHAR(26)        NOT NULL,
    user_id         BIGINT UNSIGNED NOT NULL,
    family_id       CHAR(26)        NOT NULL,
    token_hash      BINARY(32)      NOT NULL,
    expires_at      DATETIME(6)     NOT NULL,
    revoked_at      DATETIME(6)     NULL,
    replaced_by_id  BIGINT UNSIGNED NULL,
    created_at      DATETIME(6)     NOT NULL,
    last_used_at    DATETIME(6)     NULL,
    created_ip_hash BINARY(32)      NULL,
    user_agent_hash BINARY(32)      NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_refresh_tokens_public_id (public_id),
    UNIQUE KEY uk_refresh_tokens_token_hash (token_hash),
    KEY idx_refresh_tokens_user (user_id, revoked_at, expires_at),
    KEY idx_refresh_tokens_family (family_id),
    CONSTRAINT fk_refresh_tokens_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE user_action_tokens (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id         BIGINT UNSIGNED NOT NULL,
    purpose         VARCHAR(32)     NOT NULL,
    token_hash      BINARY(32)      NOT NULL,
    expires_at      DATETIME(6)     NOT NULL,
    consumed_at     DATETIME(6)     NULL,
    created_at      DATETIME(6)     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_action_tokens_lookup (user_id, purpose, expires_at),
    CONSTRAINT fk_action_tokens_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE categories (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id       CHAR(26)        NOT NULL,
    name            VARCHAR(64)     NOT NULL,
    slug            VARCHAR(80)     NOT NULL,
    description     VARCHAR(500)    NULL,
    created_at      DATETIME(6)     NOT NULL,
    updated_at      DATETIME(6)     NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_categories_public_id (public_id),
    UNIQUE KEY uk_categories_slug (slug),
    UNIQUE KEY uk_categories_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE tags (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id       CHAR(26)        NOT NULL,
    name            VARCHAR(32)     NOT NULL,
    slug            VARCHAR(80)     NOT NULL,
    created_at      DATETIME(6)     NOT NULL,
    updated_at      DATETIME(6)     NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_tags_public_id (public_id),
    UNIQUE KEY uk_tags_slug (slug),
    UNIQUE KEY uk_tags_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE posts (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id       CHAR(26)        NOT NULL,
    author_id       BIGINT UNSIGNED NOT NULL,
    title           VARCHAR(200)    NOT NULL,
    slug            VARCHAR(200)    NOT NULL,
    summary         VARCHAR(500)    NULL,
    content_markdown LONGTEXT        NOT NULL,
    content_html    LONGTEXT        NOT NULL,
    cover_url       VARCHAR(512)    NULL,
    status          VARCHAR(16)     NOT NULL DEFAULT 'draft',
    visibility      VARCHAR(16)     NOT NULL DEFAULT 'public',
    content_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    published_at    DATETIME(6)     NULL,
    created_at      DATETIME(6)     NOT NULL,
    updated_at      DATETIME(6)     NOT NULL,
    deleted_at      DATETIME(6)     NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_posts_public_id (public_id),
    UNIQUE KEY uk_posts_slug (slug),
    KEY idx_posts_status_published (status, published_at),
    KEY idx_posts_author (author_id, status),
    KEY idx_posts_updated_at (updated_at),
    CONSTRAINT fk_posts_author FOREIGN KEY (author_id) REFERENCES users (id) ON DELETE RESTRICT,
    -- 已发布文章必须有时间戳，避免依赖应用层保证。
    CONSTRAINT chk_posts_published_when_published CHECK (
        status <> 'published' OR published_at IS NOT NULL
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE post_categories (
    post_id         BIGINT UNSIGNED NOT NULL,
    category_id     BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (post_id, category_id),
    CONSTRAINT fk_pc_post FOREIGN KEY (post_id) REFERENCES posts (id) ON DELETE CASCADE,
    CONSTRAINT fk_pc_category FOREIGN KEY (category_id) REFERENCES categories (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE post_tags (
    post_id         BIGINT UNSIGNED NOT NULL,
    tag_id          BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (post_id, tag_id),
    CONSTRAINT fk_pt_post FOREIGN KEY (post_id) REFERENCES posts (id) ON DELETE CASCADE,
    CONSTRAINT fk_pt_tag FOREIGN KEY (tag_id) REFERENCES tags (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE comments (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id       CHAR(26)        NOT NULL,
    post_id         BIGINT UNSIGNED NOT NULL,
    user_id         BIGINT UNSIGNED NOT NULL,
    parent_id       BIGINT UNSIGNED NULL,
    body_markdown   TEXT            NOT NULL,
    body_html       TEXT            NOT NULL,
    status          VARCHAR(16)     NOT NULL DEFAULT 'pending',
    moderated_by    BIGINT UNSIGNED NULL,
    moderated_at    DATETIME(6)     NULL,
    created_ip_hash BINARY(32)      NULL,
    created_at      DATETIME(6)     NOT NULL,
    updated_at      DATETIME(6)     NOT NULL,
    deleted_at      DATETIME(6)     NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_comments_public_id (public_id),
    KEY idx_comments_post (post_id, status, created_at),
    KEY idx_comments_user (user_id, created_at),
    KEY idx_comments_parent (parent_id),
    UNIQUE KEY uk_comments_id_post (id, post_id),
    CONSTRAINT fk_comments_post FOREIGN KEY (post_id) REFERENCES posts (id) ON DELETE CASCADE,
    CONSTRAINT fk_comments_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_comments_parent FOREIGN KEY (parent_id, post_id) REFERENCES comments (id, post_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE background_jobs (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id       CHAR(26)        NOT NULL,
    job_type        VARCHAR(64)     NOT NULL,
    dedup_key       VARCHAR(128)    NULL,
    payload_json    JSON            NOT NULL,
    status          VARCHAR(16)     NOT NULL DEFAULT 'pending',
    priority        INT             NOT NULL DEFAULT 0,
    attempts        INT             NOT NULL DEFAULT 0,
    max_attempts    INT             NOT NULL DEFAULT 5,
    run_after       DATETIME(6)     NOT NULL,
    locked_by       CHAR(26)        NULL,
    locked_at       DATETIME(6)     NULL,
    last_error      VARCHAR(1000)   NULL,
    created_at      DATETIME(6)     NOT NULL,
    updated_at      DATETIME(6)     NOT NULL,
    finished_at     DATETIME(6)     NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_jobs_public_id (public_id),
    UNIQUE KEY uk_jobs_dedup (job_type, dedup_key),
    KEY idx_jobs_claim (status, run_after, priority)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE audit_logs (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    actor_user_id   BIGINT UNSIGNED NULL,
    action          VARCHAR(64)     NOT NULL,
    resource_type   VARCHAR(32)     NOT NULL,
    resource_id     VARCHAR(64)     NOT NULL,
    request_id      VARCHAR(64)     NULL,
    ip_hash         BINARY(32)      NULL,
    before_json     JSON            NULL,
    after_json      JSON            NULL,
    created_at      DATETIME(6)     NOT NULL,
    PRIMARY KEY (id),
    KEY idx_audit_actor (actor_user_id, created_at),
    KEY idx_audit_resource (resource_type, resource_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
