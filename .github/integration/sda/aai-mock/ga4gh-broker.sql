CREATE TABLE IF NOT EXISTS visas(
    id              BIGINT AUTO_INCREMENT NOT NULL,
    user_id         VARCHAR(255)          NOT NULL,
    source          VARCHAR(1024)         NOT NULL,
    linked_identity VARCHAR(1024)         NOT NULL,
    exp             datetime              NOT NULL,
    jwt             LONGTEXT              NOT NULL,
    CONSTRAINT pk_visas PRIMARY KEY (id)
);

CREATE INDEX idx_visaentity_user_id_source ON visas (user_id, source);