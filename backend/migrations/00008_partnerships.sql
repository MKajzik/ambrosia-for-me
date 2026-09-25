-- +goose Up
CREATE TABLE partnerships (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_a            uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    user_b            uuid REFERENCES users (id) ON DELETE CASCADE,
    status            text NOT NULL CHECK (status IN ('pending', 'active')),
    invite_code_hash  bytea,
    invite_expires_at timestamptz,
    created_by        uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CHECK (user_b IS NULL OR user_b <> user_a),
    CHECK (status <> 'pending' OR (user_b IS NULL AND invite_code_hash IS NOT NULL AND invite_expires_at IS NOT NULL)),
    CHECK (status <> 'active' OR (user_b IS NOT NULL AND invite_code_hash IS NULL AND invite_expires_at IS NULL))
);

-- One active partnership per user, per column. A user who is user_a in one
-- active row and user_b in another cannot be expressed as an index:
-- Partners.Accept locks both users rows and re-checks before it activates.
CREATE UNIQUE INDEX partnerships_active_user_a_idx ON partnerships (user_a) WHERE status = 'active';
CREATE UNIQUE INDEX partnerships_active_user_b_idx ON partnerships (user_b) WHERE status = 'active';
CREATE UNIQUE INDEX partnerships_pending_user_a_idx ON partnerships (user_a) WHERE status = 'pending';
CREATE UNIQUE INDEX partnerships_invite_code_hash_idx ON partnerships (invite_code_hash) WHERE invite_code_hash IS NOT NULL;

CREATE TRIGGER partnerships_set_updated_at
    BEFORE UPDATE ON partnerships
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE partnerships;
