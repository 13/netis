-- Session tokens are stored as their SHA-256 digest (hex) instead of in the
-- clear, so a copied database file or backup does not hand out live sessions.
--
-- Existing rows hold raw tokens that no lookup will match any more, and SQL
-- has no portable way to hash them in place, so they are dropped: everyone
-- signed in when this runs simply signs in again. The column is renamed so its
-- contents are not mistaken for the credential itself.
DELETE FROM session;
ALTER TABLE session RENAME COLUMN token TO token_hash;
