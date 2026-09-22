-- +goose Up
CREATE TABLE users (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email            citext NOT NULL,
    password_hash    text,
    apple_sub        text,
    display_name     text NOT NULL,
    target_kcal      double precision CHECK (target_kcal > 0),
    target_protein_g double precision CHECK (target_protein_g >= 0),
    target_carbs_g   double precision CHECK (target_carbs_g >= 0),
    target_fat_g     double precision CHECK (target_fat_g >= 0),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_email_key UNIQUE (email),
    CONSTRAINT users_apple_sub_key UNIQUE (apple_sub),
    CONSTRAINT users_has_credential CHECK (password_hash IS NOT NULL OR apple_sub IS NOT NULL)
);

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE users;
