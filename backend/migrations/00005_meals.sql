-- +goose Up
CREATE TABLE meals (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id            uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name                text NOT NULL,
    notes               text,
    servings            double precision NOT NULL CHECK (servings > 0),
    shared_with_partner boolean NOT NULL DEFAULT false,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX meals_owner_id_idx ON meals (owner_id);

CREATE TRIGGER meals_set_updated_at
    BEFORE UPDATE ON meals
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE meal_ingredients (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    meal_id       uuid NOT NULL REFERENCES meals (id) ON DELETE CASCADE,
    ingredient_id uuid NOT NULL REFERENCES ingredients (id),
    quantity      double precision NOT NULL CHECK (quantity > 0),
    unit          text NOT NULL CHECK (unit IN ('g', 'ml', 'piece')),
    position      integer NOT NULL CHECK (position >= 0),
    UNIQUE (meal_id, position)
);

CREATE INDEX meal_ingredients_meal_id_idx ON meal_ingredients (meal_id);
CREATE INDEX meal_ingredients_ingredient_id_idx ON meal_ingredients (ingredient_id);

-- +goose Down
DROP TABLE meal_ingredients;
DROP TABLE meals;
