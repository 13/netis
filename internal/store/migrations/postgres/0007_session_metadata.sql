-- Session provenance, so a user can see and revoke their own sessions.
--
-- All three are nullable: sessions created before this migration have no
-- record of where they came from, and inventing one would be a lie. created_at
-- is written by the login handler rather than defaulted, so every session it
-- creates carries an explicit UTC RFC3339 timestamp like the rest of the
-- schema's time columns.
ALTER TABLE session ADD COLUMN created_at TEXT;
ALTER TABLE session ADD COLUMN ip TEXT;
ALTER TABLE session ADD COLUMN user_agent TEXT;
