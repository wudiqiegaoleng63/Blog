-- 0002_ai_indexing.up.sql
-- Stage 3 文章索引状态；向量与 chunk 正文存放在 Milvus。

CREATE TABLE ai_documents (
    id                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    post_id           BIGINT UNSIGNED NOT NULL,
    content_version   BIGINT UNSIGNED NOT NULL,
    embedding_model   VARCHAR(128)    NOT NULL,
    chunk_count       INT UNSIGNED    NOT NULL DEFAULT 0,
    status            VARCHAR(16)     NOT NULL DEFAULT 'pending',
    last_error        VARCHAR(1000)   NULL,
    indexed_at        DATETIME(6)     NULL,
    created_at        DATETIME(6)     NOT NULL,
    updated_at        DATETIME(6)     NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_ai_documents_post (post_id),
    KEY idx_ai_documents_status_updated (status, updated_at),
    CONSTRAINT fk_ai_documents_post FOREIGN KEY (post_id) REFERENCES posts (id) ON DELETE CASCADE,
    CONSTRAINT chk_ai_documents_status CHECK (status IN ('pending', 'indexed', 'removed', 'failed'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
