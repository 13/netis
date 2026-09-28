-- OpenID Connect login links a user to an identity at the provider by the
-- issuer and the subject ("sub") claim, the one pair the spec promises is
-- stable and unique. Both are NULL for password-only accounts; NULLs are
-- distinct under a unique index on both backends, so any number of those fit.
ALTER TABLE "user" ADD COLUMN oidc_issuer TEXT;
ALTER TABLE "user" ADD COLUMN oidc_subject TEXT;
CREATE UNIQUE INDEX user_oidc_identity ON "user" (oidc_issuer, oidc_subject);
