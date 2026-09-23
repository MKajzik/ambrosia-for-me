-- +goose Up
CREATE TABLE shopping_lists (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id            uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name                text NOT NULL,
    shared_with_partner boolean NOT NULL DEFAULT false,
    source_from         date,
    source_to           date,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CHECK ((source_from IS NULL) = (source_to IS NULL)),
    CHECK (source_to >= source_from)
);

CREATE INDEX shopping_lists_owner_id_created_at_idx ON shopping_lists (owner_id, created_at DESC, id DESC);

CREATE TRIGGER shopping_lists_set_updated_at
    BEFORE UPDATE ON shopping_lists
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE shopping_items (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    list_id       uuid NOT NULL REFERENCES shopping_lists (id) ON DELETE CASCADE,
    ingredient_id uuid REFERENCES ingredients (id) ON DELETE SET NULL,
    name          text NOT NULL,
    quantity      double precision CHECK (quantity > 0),
    unit          text CHECK (unit IN ('g', 'ml', 'piece')),
    category      text NOT NULL CHECK (category IN (
        'produce', 'dairy_eggs', 'meat_seafood', 'grains_bread', 'legumes_nuts_seeds',
        'condiments_oils', 'spices_herbs', 'beverages', 'sweets_snacks', 'other'
    )),
    checked       boolean NOT NULL DEFAULT false,
    checked_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    position      integer NOT NULL CHECK (position >= 0),
    version       integer NOT NULL DEFAULT 1 CHECK (version >= 1),
    origin        text NOT NULL CHECK (origin IN ('generated', 'manual')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX shopping_items_list_id_idx ON shopping_items (list_id);
CREATE INDEX shopping_items_ingredient_id_idx ON shopping_items (ingredient_id);
CREATE INDEX shopping_items_checked_by_idx ON shopping_items (checked_by);

CREATE TRIGGER shopping_items_set_updated_at
    BEFORE UPDATE ON shopping_items
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE shopping_items;
DROP TABLE shopping_lists;
