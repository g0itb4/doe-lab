DROP TABLE IF EXISTS readings;
DROP TRIGGER IF EXISTS envelopes_immutable ON envelopes;
DROP TABLE IF EXISTS envelopes;
DROP FUNCTION IF EXISTS envelopes_supersede_only();
DROP TYPE IF EXISTS binding_constraint;
DROP TYPE IF EXISTS envelope_source;
DROP TABLE IF EXISTS site_profiles;
