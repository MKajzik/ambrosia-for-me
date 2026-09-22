-- +goose Up
CREATE TYPE nutrient_key AS ENUM (
    'calories', 'protein', 'carbohydrates', 'sugar', 'fibre', 'fat', 'saturated_fat',
    'sodium', 'potassium', 'calcium', 'iron', 'magnesium', 'zinc',
    'vitamin_a', 'vitamin_c', 'vitamin_d', 'vitamin_b12', 'folate'
);

CREATE TABLE ingredients (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name             text NOT NULL,
    category         text NOT NULL CHECK (category IN (
        'produce', 'dairy_eggs', 'meat_seafood', 'grains_bread', 'legumes_nuts_seeds',
        'condiments_oils', 'spices_herbs', 'beverages', 'sweets_snacks', 'other'
    )),
    owner_id         uuid REFERENCES users (id) ON DELETE CASCADE,
    usda_fdc_id      integer UNIQUE,
    grams_per_piece  double precision CHECK (grams_per_piece > 0),
    density_g_per_ml double precision CHECK (density_g_per_ml > 0),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ingredients_name_trgm_idx ON ingredients USING gin (name gin_trgm_ops);
CREATE INDEX ingredients_owner_id_idx ON ingredients (owner_id);
CREATE INDEX ingredients_category_idx ON ingredients (category);

CREATE TRIGGER ingredients_set_updated_at
    BEFORE UPDATE ON ingredients
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE ingredient_nutrients (
    ingredient_id   uuid NOT NULL REFERENCES ingredients (id) ON DELETE CASCADE,
    nutrient_key    nutrient_key NOT NULL,
    amount_per_100g double precision NOT NULL CHECK (amount_per_100g >= 0),
    PRIMARY KEY (ingredient_id, nutrient_key)
);

-- +goose Down
DROP TABLE ingredient_nutrients;
DROP TABLE ingredients;
DROP TYPE nutrient_key;
