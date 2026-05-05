-- Image library plus the structural tables for jobs, annotation_sets,
-- annotations, model_versions, dataset_snapshots. Phase 1 only writes to
-- `images`; the others are reserved with the columns the spec demands so
-- subsequent phases never need a "schema present but no handler"
-- mismatch (section 18).

CREATE TYPE image_status AS ENUM ('uploading', 'ready', 'errored', 'archived');

CREATE TABLE images (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    uploaded_by   UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    storage_key   TEXT NOT NULL,                   -- e.g. orgs/<org_id>/images/<id>.jpg
    storage_etag  TEXT,
    content_type  TEXT NOT NULL,
    byte_size     BIGINT NOT NULL CHECK (byte_size > 0 AND byte_size <= 52428800),
    width         INTEGER,
    height        INTEGER,
    sha256        CHAR(64),
    status        image_status NOT NULL DEFAULT 'uploading',
    metadata      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ
);

CREATE INDEX idx_images_org_created ON images (org_id, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_images_status ON images (status) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX idx_images_storage_key ON images (storage_key);
CREATE UNIQUE INDEX idx_images_org_sha ON images (org_id, sha256) WHERE sha256 IS NOT NULL AND deleted_at IS NULL;

CREATE TRIGGER trg_images_updated BEFORE UPDATE ON images
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE labels (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    color       CHAR(7) NOT NULL,                 -- hex like #4a8ff5
    description TEXT,
    archived    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

CREATE TABLE annotation_sets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    image_id    UUID NOT NULL REFERENCES images(id) ON DELETE CASCADE,
    -- Optimistic concurrency control (section 10). Bump on every PATCH;
    -- the API returns ETag = version and rejects If-Match mismatches with 409.
    version     BIGINT NOT NULL DEFAULT 1,
    notes       TEXT,
    created_by  UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, image_id)
);

CREATE INDEX idx_annotation_sets_image ON annotation_sets (image_id);

CREATE TRIGGER trg_annotation_sets_updated BEFORE UPDATE ON annotation_sets
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TYPE annotation_kind AS ENUM ('bbox', 'polygon', 'mask', 'point');

CREATE TABLE annotations (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id            UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    annotation_set_id UUID NOT NULL REFERENCES annotation_sets(id) ON DELETE CASCADE,
    label_id          UUID REFERENCES labels(id) ON DELETE RESTRICT,
    kind              annotation_kind NOT NULL,
    geometry          JSONB NOT NULL,            -- GeoJSON-ish; {type, coordinates}
    mask_storage_key  TEXT,                      -- present when kind='mask'
    ai_score          DOUBLE PRECISION,          -- model confidence
    quality_score     DOUBLE PRECISION,          -- aggregate quality
    model_used        TEXT,                      -- e.g. "sam-2.1@v3"
    human_accepted    BOOLEAN,
    created_by        UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at        TIMESTAMPTZ
);

CREATE INDEX idx_annotations_set ON annotations (annotation_set_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_annotations_org_score ON annotations (org_id, ai_score) WHERE deleted_at IS NULL;
CREATE INDEX idx_annotations_label ON annotations (label_id) WHERE deleted_at IS NULL;

CREATE TRIGGER trg_annotations_updated BEFORE UPDATE ON annotations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TYPE job_state AS ENUM ('pending', 'running', 'succeeded', 'failed', 'cancelled');
CREATE TYPE job_type AS ENUM ('auto', 'box', 'points', 'polygon', 'detect', 'train', 'export');

CREATE TABLE jobs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    image_id     UUID REFERENCES images(id) ON DELETE SET NULL,
    submitted_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    type         job_type NOT NULL,
    state        job_state NOT NULL DEFAULT 'pending',
    payload      JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- Idempotency (section 11). SHA-256 of (org_id, image_id, type, payload).
    dedup_key    CHAR(64) NOT NULL,
    attempt      SMALLINT NOT NULL DEFAULT 0,
    error        TEXT,
    result       JSONB,
    started_at   TIMESTAMPTZ,
    finished_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_jobs_dedup_window ON jobs (dedup_key)
    WHERE state IN ('pending', 'running');
CREATE INDEX idx_jobs_org_state ON jobs (org_id, state, created_at DESC);

CREATE TRIGGER trg_jobs_updated BEFORE UPDATE ON jobs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Model registry (Phase 3 writes, Phase 7 promotes).
CREATE TABLE model_versions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID REFERENCES organizations(id) ON DELETE CASCADE,
    family          TEXT NOT NULL CHECK (family IN ('sam', 'yolo')),
    semver          TEXT NOT NULL,
    onnx_key        TEXT,
    weights_key     TEXT,
    mean_iou        DOUBLE PRECISION,
    map50           DOUBLE PRECISION,
    embedding_dim   INTEGER,                  -- v2: validate against pgvector column
    is_active       BOOLEAN NOT NULL DEFAULT FALSE,
    promoted_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, family, semver)
);

-- One active version per (org, family). NULL org_id is the global default.
CREATE UNIQUE INDEX idx_model_versions_active
    ON model_versions (COALESCE(org_id::text, 'global'), family)
    WHERE is_active;

-- Dataset snapshots (Phase 7).
CREATE TABLE dataset_snapshots (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    semver      TEXT NOT NULL,
    storage_key TEXT NOT NULL,
    format      TEXT NOT NULL CHECK (format IN ('coco', 'yolo', 'voc', 'tfrecord')),
    image_count INTEGER NOT NULL,
    label_count INTEGER NOT NULL,
    created_by  UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, semver, format)
);

CREATE INDEX idx_dataset_snapshots_org ON dataset_snapshots (org_id, created_at DESC);
