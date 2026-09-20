-- Conventions: the derived machinery every business table gets for free.
-- The derivation table and rationale live in packages/db/README.md.
--
-- Adapted from the Nest project's conventions migration. Two differences:
--   * the marker of a business table is an `updated_at` column (Nest uses
--     `metadata`), so immutable tables and hypertables are never targets;
--   * the audit actor is a name ("operator", "engine", "import"), not a user
--     id, because this system has scoped tokens and no user table.

CREATE EXTENSION IF NOT EXISTS timescaledb;

-- uuidv7 polyfill for Postgres 17; a no-op on 18+.
-- floor(), not a rounding cast: rounding can land the millisecond one ahead of
-- the clock and break the time-ordering the type exists for.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_proc WHERE proname = 'uuidv7' AND pronargs = 0) THEN
    CREATE FUNCTION public.uuidv7() RETURNS uuid AS $fn$
    SELECT encode(
      set_bit(
        set_bit(
          overlay(
            uuid_send(gen_random_uuid())
            PLACING substring(int8send(floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint) FROM 3)
            FROM 1 FOR 6
          ),
          52, 1
        ),
        53, 1
      ),
      'hex')::uuid;
    $fn$ LANGUAGE sql VOLATILE;
  END IF;
END
$$;

-- ---------------------------------------------------------------------------
-- The registries
-- ---------------------------------------------------------------------------

-- None of these registries carries `updated_at`, so the derivation ignores them.

CREATE TYPE convention AS ENUM ('updated_at', 'history', 'soft_delete_cascade');

CREATE TABLE convention_exemptions (
  table_name text       NOT NULL,
  convention convention NOT NULL,
  reason     text       NOT NULL,
  created_at timestamptz(3) NOT NULL DEFAULT now(),
  PRIMARY KEY (table_name, convention),
  CONSTRAINT convention_exemptions_reason_present CHECK (reason <> '')
);

COMMENT ON TABLE convention_exemptions IS
  'Tables deliberately excluded from a derived convention. Every row needs a reason.';

-- For personal data; secrets are covered by the deny-list in record_row_history().
CREATE TABLE audit_redactions (
  table_name  text NOT NULL,
  column_name text NOT NULL,
  reason      text NOT NULL,
  created_at  timestamptz(3) NOT NULL DEFAULT now(),
  PRIMARY KEY (table_name, column_name),
  CONSTRAINT audit_redactions_reason_present CHECK (reason <> '')
);

COMMENT ON TABLE audit_redactions IS
  'Per-column redaction for row_history. Secrets are already denied unconditionally.';

-- How long a table's audit rows are kept. Absent means forever.
CREATE TABLE row_history_retention (
  table_name text     NOT NULL PRIMARY KEY,
  keep       interval NOT NULL,
  reason     text     NOT NULL,
  created_at timestamptz(3) NOT NULL DEFAULT now(),
  CONSTRAINT row_history_retention_keep_positive CHECK (keep > interval '0'),
  CONSTRAINT row_history_retention_reason_present CHECK (reason <> '')
);

-- ---------------------------------------------------------------------------
-- The audit log
-- ---------------------------------------------------------------------------

CREATE TYPE row_op AS ENUM ('insert', 'update', 'delete', 'truncate');

CREATE TABLE row_history (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  table_name text        NOT NULL,
  row_id     uuid,
  op         row_op      NOT NULL,
  changed_at timestamptz(3) NOT NULL DEFAULT now(),
  -- From the app.actor GUC; NULL means a by-hand write (psql).
  changed_by text,
  -- Free-text note about the writing context, e.g. 'rpc:UpdateSite'.
  actor_note text        NOT NULL DEFAULT '',
  old_row    jsonb,
  new_row    jsonb,
  CONSTRAINT row_history_row_id_present CHECK ((op = 'truncate') = (row_id IS NULL))
);

-- "Everything that ever happened to this row."
CREATE INDEX row_history_row_idx
  ON row_history (table_name, row_id, changed_at DESC, id DESC);
-- The recent-activity feed, and the scan prune_row_history() walks.
CREATE INDEX row_history_recent_idx ON row_history (changed_at DESC, id DESC);

-- ---------------------------------------------------------------------------
-- The trigger functions
-- ---------------------------------------------------------------------------

CREATE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
  NEW.updated_at := now();
  RETURN NEW;
END $$ LANGUAGE plpgsql;

COMMENT ON FUNCTION set_updated_at() IS
  'Owns updated_at. Nothing else may set it; a hand-written value is a lie the next UPDATE corrects.';

-- `true` scopes the GUC to the transaction: safe on a pooled connection.
CREATE FUNCTION set_actor(actor text, note text DEFAULT '') RETURNS void AS $$
  SELECT set_config('app.actor', COALESCE(actor, ''), true),
         set_config('app.actor_note', COALESCE(note, ''), true);
  SELECT NULL::void;
$$ LANGUAGE sql;

CREATE FUNCTION record_row_history() RETURNS trigger AS $$
DECLARE
  -- Never auditable, whatever the registry says: no row can un-redact a secret.
  always_strip constant text[] := ARRAY[
    'password_hash', 'token_hash', 'secret', 'api_key', 'refresh_token', 'access_token'
  ];
  excluded text[];
  old_j jsonb;
  new_j jsonb;
  base  jsonb;
BEGIN
  SELECT always_strip || COALESCE(array_agg(column_name), '{}')
    INTO excluded
    FROM audit_redactions
   WHERE table_name = TG_TABLE_NAME;

  IF TG_OP <> 'INSERT' THEN old_j := to_jsonb(OLD) - excluded; END IF;
  IF TG_OP <> 'DELETE' THEN new_j := to_jsonb(NEW) - excluded; END IF;

  -- Skip UPDATEs that changed nothing but updated_at.
  IF TG_OP = 'UPDATE' AND old_j - 'updated_at' = new_j - 'updated_at' THEN
    RETURN NULL;
  END IF;

  base := COALESCE(new_j, old_j);
  INSERT INTO row_history (table_name, row_id, op, changed_by, actor_note, old_row, new_row)
  VALUES (
    TG_TABLE_NAME,
    (base->>'id')::uuid,
    lower(TG_OP)::row_op,
    NULLIF(current_setting('app.actor', true), ''),
    COALESCE(NULLIF(current_setting('app.actor_note', true), ''), ''),
    old_j, new_j
  );
  RETURN NULL;
END $$ LANGUAGE plpgsql;

CREATE FUNCTION record_truncate_history() RETURNS trigger AS $$
BEGIN
  INSERT INTO row_history (table_name, row_id, op, changed_by, actor_note)
  VALUES (TG_TABLE_NAME, NULL, 'truncate',
          NULLIF(current_setting('app.actor', true), ''),
          COALESCE(NULLIF(current_setting('app.actor_note', true), ''), ''));
  RETURN NULL;
END $$ LANGUAGE plpgsql;

CREATE FUNCTION reject_row_history_mutation() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'row_history is append-only (attempted %)', TG_OP
    USING ERRCODE = 'restrict_violation';
END $$ LANGUAGE plpgsql;

-- Soft-delete propagation; edges come from pg_constraint (see the README).
-- The in-step unnest of (conkey, confkey) is what makes composite FKs work: it
-- picks out the child column aligned with the parent's `id`. A child with no
-- deleted_at is hard-deleted instead.
CREATE FUNCTION cascade_soft_delete() RETURNS trigger AS $$
DECLARE
  edge record;
BEGIN
  FOR edge IN
    SELECT c.conrelid::regclass::text AS child_table,
           ca.attname                 AS child_column,
           EXISTS (
             SELECT 1 FROM pg_attribute d
              WHERE d.attrelid = c.conrelid
                AND d.attname = 'deleted_at'
                AND d.attnum > 0 AND NOT d.attisdropped
           ) AS child_soft_deletes
      FROM pg_constraint c
      CROSS JOIN LATERAL unnest(c.conkey, c.confkey) AS u(child_attnum, parent_attnum)
      JOIN pg_attribute ca ON ca.attrelid = c.conrelid  AND ca.attnum = u.child_attnum
      JOIN pg_attribute pa ON pa.attrelid = c.confrelid AND pa.attnum = u.parent_attnum
     WHERE c.contype = 'f'
       AND c.confrelid = TG_RELID
       AND c.confdeltype = 'c'          -- ON DELETE CASCADE
       AND pa.attname = 'id'
  LOOP
    IF edge.child_soft_deletes THEN
      EXECUTE format(
        'UPDATE %I SET deleted_at = $1 WHERE %I = $2 AND deleted_at IS NULL',
        edge.child_table, edge.child_column)
        USING NEW.deleted_at, NEW.id;
    ELSE
      EXECUTE format('DELETE FROM %I WHERE %I = $1', edge.child_table, edge.child_column)
        USING NEW.id;
    END IF;
  END LOOP;
  RETURN NULL;
END $$ LANGUAGE plpgsql;

-- ---------------------------------------------------------------------------
-- The derivation
-- ---------------------------------------------------------------------------

CREATE FUNCTION has_column(rel oid, col text) RETURNS boolean AS $$
  SELECT EXISTS (
    SELECT 1 FROM pg_attribute
     WHERE attrelid = rel AND attname = col AND attnum > 0 AND NOT attisdropped
  );
$$ LANGUAGE sql STABLE;

CREATE FUNCTION has_trigger(table_name text, trigger_name text) RETURNS boolean AS $$
  SELECT EXISTS (
    SELECT 1 FROM pg_trigger t
      JOIN pg_class c ON c.oid = t.tgrelid
      JOIN pg_namespace n ON n.oid = c.relnamespace
     WHERE NOT t.tgisinternal AND n.nspname = 'public'
       AND c.relname = $1 AND t.tgname = $2
  );
$$ LANGUAGE sql STABLE;

-- The WHEN clause for set_updated_at, built from STORED columns only:
-- `old.* IS DISTINCT FROM new.*` is rejected on tables with generated columns.
CREATE FUNCTION changed_predicate(rel oid) RETURNS text AS $$
  SELECT format('(ROW(%s) IS DISTINCT FROM ROW(%s))',
                string_agg('old.' || quote_ident(attname), ', ' ORDER BY attnum),
                string_agg('new.' || quote_ident(attname), ', ' ORDER BY attnum))
    FROM pg_attribute
   WHERE attrelid = rel AND attnum > 0 AND NOT attisdropped AND attgenerated = '';
$$ LANGUAGE sql STABLE;

-- One definition shared by apply_conventions() and assert_conventions().
-- Must stay a FUNCTION, not a view: sqlc reads this directory as its schema and
-- a view over pg_class breaks codegen (it does not parse function bodies).
CREATE FUNCTION convention_targets()
RETURNS TABLE (
  table_name       text,
  wants_updated_at boolean,
  wants_cascade    boolean,
  changed_when     text
) AS $$
  SELECT
    c.relname::text,
    has_column(c.oid, 'updated_at'),
    has_column(c.oid, 'deleted_at'),
    changed_predicate(c.oid)
  FROM pg_class c
  JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE c.relkind = 'r'
    AND n.nspname = 'public'
    -- The marker. A table with no updated_at is immutable (its rows are their
    -- own history), a registry, or a hypertable, and is never a target.
    AND has_column(c.oid, 'updated_at');
$$ LANGUAGE sql STABLE;

-- Install a trigger only if its definition would change, so a no-op call takes
-- no ACCESS EXCLUSIVE lock. Compares an md5 fingerprint in the trigger's
-- COMMENT, never pg_get_triggerdef (normalised, never matches; see the README).
CREATE FUNCTION ensure_trigger(
  trigger_name text,
  table_name   text,
  definition   text
) RETURNS boolean AS $$
DECLARE
  trigger_oid oid;
  want        text := 'doelab:conventions:' || md5(definition);
BEGIN
  SELECT t.oid INTO trigger_oid
    FROM pg_trigger t
    JOIN pg_class c ON c.oid = t.tgrelid
    JOIN pg_namespace n ON n.oid = c.relnamespace
   WHERE NOT t.tgisinternal
     AND n.nspname = 'public'
     AND c.relname = table_name
     AND t.tgname = trigger_name;

  IF trigger_oid IS NOT NULL AND obj_description(trigger_oid, 'pg_trigger') = want THEN
    RETURN false;
  END IF;

  IF trigger_oid IS NOT NULL THEN
    EXECUTE format('DROP TRIGGER %I ON %I', trigger_name, table_name);
  END IF;
  EXECUTE definition;
  EXECUTE format('COMMENT ON TRIGGER %I ON %I IS %L', trigger_name, table_name, want);
  RETURN true;
END $$ LANGUAGE plpgsql;

-- Drop a convention trigger the table no longer earns.
CREATE FUNCTION drop_trigger_if_present(trigger_name text, table_name text)
RETURNS boolean AS $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM pg_trigger t
      JOIN pg_class c ON c.oid = t.tgrelid
      JOIN pg_namespace n ON n.oid = c.relnamespace
     WHERE NOT t.tgisinternal AND n.nspname = 'public'
       AND c.relname = table_name AND t.tgname = trigger_name
  ) THEN
    EXECUTE format('DROP TRIGGER %I ON %I', trigger_name, table_name);
    RETURN true;
  END IF;
  RETURN false;
END $$ LANGUAGE plpgsql;

-- Reconcile every convention trigger with the catalog. Idempotent and cheap.
CREATE FUNCTION apply_conventions() RETURNS integer AS $$
DECLARE
  t       record;
  changed integer := 0;
  exempt  boolean;
BEGIN
  FOR t IN SELECT * FROM convention_targets() ORDER BY table_name LOOP

    ---- updated_at ----------------------------------------------------------
    SELECT EXISTS (SELECT 1 FROM convention_exemptions
                    WHERE table_name = t.table_name AND convention = 'updated_at')
      INTO exempt;

    IF t.wants_updated_at AND NOT exempt THEN
      -- Guarded on a real change, so a no-op UPDATE does not bump the timestamp.
      changed := changed + ensure_trigger(
        t.table_name || '_set_updated_at', t.table_name,
        format('CREATE TRIGGER %I BEFORE UPDATE ON %I FOR EACH ROW '
               'WHEN %s EXECUTE FUNCTION set_updated_at()',
               t.table_name || '_set_updated_at', t.table_name, t.changed_when))::int;
    ELSE
      changed := changed + drop_trigger_if_present(
        t.table_name || '_set_updated_at', t.table_name)::int;
    END IF;

    ---- audit ---------------------------------------------------------------
    SELECT EXISTS (SELECT 1 FROM convention_exemptions
                    WHERE table_name = t.table_name AND convention = 'history')
      INTO exempt;

    IF NOT exempt THEN
      changed := changed + ensure_trigger(
        t.table_name || '_history', t.table_name,
        format('CREATE TRIGGER %I AFTER INSERT OR DELETE OR UPDATE ON %I '
               'FOR EACH ROW EXECUTE FUNCTION record_row_history()',
               t.table_name || '_history', t.table_name))::int;
      changed := changed + ensure_trigger(
        t.table_name || '_truncate_history', t.table_name,
        format('CREATE TRIGGER %I AFTER TRUNCATE ON %I '
               'FOR EACH STATEMENT EXECUTE FUNCTION record_truncate_history()',
               t.table_name || '_truncate_history', t.table_name))::int;
    ELSE
      changed := changed + drop_trigger_if_present(t.table_name || '_history', t.table_name)::int;
      changed := changed + drop_trigger_if_present(t.table_name || '_truncate_history', t.table_name)::int;
    END IF;

    ---- soft-delete cascade -------------------------------------------------
    SELECT EXISTS (SELECT 1 FROM convention_exemptions
                    WHERE table_name = t.table_name AND convention = 'soft_delete_cascade')
      INTO exempt;

    IF t.wants_cascade AND NOT exempt THEN
      changed := changed + ensure_trigger(
        t.table_name || '_cascade_soft_delete', t.table_name,
        format('CREATE TRIGGER %I AFTER UPDATE OF deleted_at ON %I FOR EACH ROW '
               'WHEN ((old.deleted_at IS NULL AND new.deleted_at IS NOT NULL)) '
               'EXECUTE FUNCTION cascade_soft_delete()',
               t.table_name || '_cascade_soft_delete', t.table_name))::int;
    ELSE
      changed := changed + drop_trigger_if_present(
        t.table_name || '_cascade_soft_delete', t.table_name)::int;
    END IF;

  END LOOP;

  RETURN changed;
END $$ LANGUAGE plpgsql;

-- Raises on the first table missing an earned trigger, and on registry rows
-- naming a table or column that no longer exists.
CREATE FUNCTION assert_conventions() RETURNS void AS $$
DECLARE
  problem text;
BEGIN
  SELECT format('%s is missing its %s trigger', table_name, missing)
    INTO problem
    FROM (
      SELECT t.table_name,
             CASE
               WHEN t.wants_updated_at
                AND NOT EXISTS (SELECT 1 FROM convention_exemptions e
                                 WHERE e.table_name = t.table_name AND e.convention = 'updated_at')
                AND NOT has_trigger(t.table_name, t.table_name || '_set_updated_at')
                 THEN 'updated_at'
               WHEN NOT EXISTS (SELECT 1 FROM convention_exemptions e
                                 WHERE e.table_name = t.table_name AND e.convention = 'history')
                AND NOT has_trigger(t.table_name, t.table_name || '_history')
                 THEN 'history'
               WHEN t.wants_cascade
                AND NOT EXISTS (SELECT 1 FROM convention_exemptions e
                                 WHERE e.table_name = t.table_name AND e.convention = 'soft_delete_cascade')
                AND NOT has_trigger(t.table_name, t.table_name || '_cascade_soft_delete')
                 THEN 'soft-delete cascade'
             END AS missing
        FROM convention_targets() t
    ) q
   WHERE missing IS NOT NULL
   LIMIT 1;

  IF problem IS NOT NULL THEN
    RAISE EXCEPTION 'schema conventions: %', problem
      USING HINT = 'End the migration with: SELECT apply_conventions();';
  END IF;

  SELECT format('convention_exemptions names table %I, which does not exist', e.table_name)
    INTO problem
    FROM convention_exemptions e
   WHERE to_regclass('public.' || quote_ident(e.table_name)) IS NULL
   LIMIT 1;
  IF problem IS NOT NULL THEN
    RAISE EXCEPTION 'schema conventions: %', problem;
  END IF;

  SELECT format('audit_redactions names %I.%I, which does not exist', r.table_name, r.column_name)
    INTO problem
    FROM audit_redactions r
   WHERE to_regclass('public.' || quote_ident(r.table_name)) IS NULL
      OR NOT has_column(to_regclass('public.' || quote_ident(r.table_name)), r.column_name)
   LIMIT 1;
  IF problem IS NOT NULL THEN
    RAISE EXCEPTION 'schema conventions: %', problem;
  END IF;
END $$ LANGUAGE plpgsql;

-- Retention: honours row_history_retention. Nothing schedules this yet.
CREATE FUNCTION prune_row_history() RETURNS bigint AS $$
DECLARE
  deleted bigint := 0;
  r       record;
  n       bigint;
BEGIN
  FOR r IN SELECT table_name, keep FROM row_history_retention LOOP
    -- The append-only guard blocks DELETE; SET LOCAL replica lifts it, scoped
    -- to the surrounding transaction.
    SET LOCAL session_replication_role = replica;
    DELETE FROM row_history
     WHERE table_name = r.table_name AND changed_at < now() - r.keep;
    GET DIAGNOSTICS n = ROW_COUNT;
    SET LOCAL session_replication_role = origin;
    deleted := deleted + n;
  END LOOP;
  RETURN deleted;
END $$ LANGUAGE plpgsql;

-- ---------------------------------------------------------------------------
-- row_history's own guards
-- ---------------------------------------------------------------------------

-- Hand-installed: row_history has no updated_at column and is not a target.
CREATE TRIGGER row_history_append_only
  BEFORE UPDATE OR DELETE ON row_history
  FOR EACH ROW EXECUTE FUNCTION reject_row_history_mutation();

CREATE TRIGGER row_history_no_truncate
  BEFORE TRUNCATE ON row_history
  FOR EACH STATEMENT EXECUTE FUNCTION reject_row_history_mutation();

SELECT apply_conventions();
