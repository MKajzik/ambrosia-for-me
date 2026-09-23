-- +goose Up
CREATE TABLE diet_templates (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id            uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name                text NOT NULL,
    day_count           integer NOT NULL CHECK (day_count >= 1),
    shared_with_partner boolean NOT NULL DEFAULT false,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX diet_templates_owner_id_idx ON diet_templates (owner_id);

CREATE TRIGGER diet_templates_set_updated_at
    BEFORE UPDATE ON diet_templates
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE template_slots (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id uuid NOT NULL REFERENCES diet_templates (id) ON DELETE CASCADE,
    day_index   integer NOT NULL CHECK (day_index >= 0),
    slot        text NOT NULL CHECK (slot IN ('breakfast', 'lunch', 'dinner', 'snack')),
    meal_id     uuid NOT NULL REFERENCES meals (id),
    portion     double precision NOT NULL DEFAULT 1 CHECK (portion > 0)
);

CREATE UNIQUE INDEX template_slots_unique_slot_idx ON template_slots (template_id, day_index, slot) WHERE slot != 'snack';
CREATE INDEX template_slots_template_id_idx ON template_slots (template_id);
CREATE INDEX template_slots_meal_id_idx ON template_slots (meal_id);

CREATE TABLE plan_entries (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    date             date NOT NULL,
    slot             text NOT NULL CHECK (slot IN ('breakfast', 'lunch', 'dinner', 'snack')),
    meal_id          uuid NOT NULL REFERENCES meals (id),
    portion          double precision NOT NULL DEFAULT 1 CHECK (portion > 0),
    from_template_id uuid REFERENCES diet_templates (id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX plan_entries_unique_slot_idx ON plan_entries (owner_id, date, slot) WHERE slot != 'snack';
CREATE INDEX plan_entries_owner_id_date_idx ON plan_entries (owner_id, date);
CREATE INDEX plan_entries_meal_id_idx ON plan_entries (meal_id);

CREATE TRIGGER plan_entries_set_updated_at
    BEFORE UPDATE ON plan_entries
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE plan_entries;
DROP TABLE template_slots;
DROP TABLE diet_templates;
