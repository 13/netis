# Netis — OpenID Connect login (F7a): Design

Date: 2026-09-28
Status: Approved (standing authorization), pre-implementation

## Purpose

Homelabs increasingly run one identity provider (Authelia, Authentik,
Keycloak, Pocket ID) in front of everything. F7a lets netis accept a login
from that provider over OpenID Connect, so users sign in once and admins stop
managing a second set of passwords. Password login stays, as the fallback and
the break-glass.

## Configuration (env, `internal/config`)

Deployment config with a client secret in it, so it lives in the environment,
not in the settings table an admin can read back.

| Variable | Default | Meaning |
| --- | --- | --- |
| `NETIS_OIDC_ISSUER` | unset | Issuer URL. Unset = SSO off; everything below is ignored. |
| `NETIS_OIDC_CLIENT_ID` | — | Required when the issuer is set. |
| `NETIS_OIDC_CLIENT_SECRET` | empty | Empty = public client (PKCE only). |
| `NETIS_OIDC_REDIRECT_URL` | derived | Callback URL registered at the IdP. |
| `NETIS_BASE_URL` | unset | When the redirect URL is unset it is `NETIS_BASE_URL` + `/auth/oidc/callback`. One of the two is required. |
| `NETIS_OIDC_ADMIN_GROUP` | unset | Group whose members get admin; everyone else viewer. Unset = SSO does not manage roles (new users are viewers, existing roles are left alone). |
| `NETIS_OIDC_GROUPS_CLAIM` | `groups` | ID-token claim holding the groups (array of strings, or one string). |
| `NETIS_OIDC_AUTO_CREATE` | `true` | Create a netis user on the first SSO login of an unknown identity. |
| `NETIS_OIDC_DISABLE_PASSWORD` | `false` | Hide the password form; see break-glass below. |

An issuer without a client id or a redirect URL is a startup error (like a bad
`NETIS_SECRET_KEY`): a half-configured SSO button that can never work is worse
than a refusal to start. Booleans accept what `strconv.ParseBool` accepts; an
unparseable value is a startup error too.

Discovery is lazy: the provider's discovery document is fetched on the first
SSO request and cached once it succeeds, so an IdP that is down while netis
starts does not keep netis from starting, and a failed fetch is retried on the
next attempt.

## Flow

Authorization code flow with PKCE (S256), scopes `openid profile email groups`
(`groups` is harmless where unsupported; Authelia and Pocket ID need it asked
for).

1. `GET /auth/oidc/login` — public. Generates state, nonce and a PKCE
   verifier, puts them in the `netis_oidc` cookie and redirects to the IdP.
   Refused (redirect to `/setup`) while no user exists: the first admin is
   always a local password account, which is also the break-glass.
2. `GET /auth/oidc/callback` — public (exempt from `requireAuth`), protected
   by the state. Checks the state against the cookie (constant time), exchanges
   the code with the verifier, verifies the ID token (signature via JWKS,
   issuer, audience = client id, expiry — go-oidc's verifier) and then the
   nonce against the cookie. The cookie is cleared whatever the outcome.
3. The identity is mapped to a local user (below), and a session is created
   exactly as a password login creates one: the same helper, hashed token,
   30-day expiry, session metadata and cookie flags.

The `netis_oidc` cookie: HttpOnly, SameSite=Lax (the IdP's redirect back is a
top-level cross-site GET, which Lax lets through; Strict would drop it),
Secure when the request is, Path `/auth/oidc/`, 10-minute max age. Its value
is JSON signed with HMAC-SHA256 under a key generated at process start; a
restart only invalidates logins in flight. Signing stops a client from editing
the link target (below) or pairing a state with a nonce of its choosing.

Callback failures are rate limited with the login per-address limiter: each
callback reserves a slot and a failed one is charged. Error pages say what
went wrong in general terms ("sign-in failed", "no netis account is linked")
and the details go to the log.

Every callback is written to the audit log (`sso.login`, or `sso.link` for a
link), with the user when one was reached and a short reason on failure. The
callback is a GET, which the audit middleware skips, so the handler records it
itself; the `POST /settings/sso/link` that starts a link is audited by the
middleware as `sso.link_start`.

## Mapping an identity to a user

`user` gains `oidc_issuer` and `oidc_subject` (nullable, unique together;
migration 0014 in both dialects). The pair `(iss, sub)` is the only thing a
login is matched on: `sub` is the one claim the spec promises is stable and
unique per issuer.

- Linked user found → sign in as it.
- Not found, auto-create on → create a user named after `preferred_username`,
  falling back to `email`, then `sub`. If a local user already has that name the
  login is refused. It is never silently attached to the existing account: a
  user who can set their own `preferred_username` at the IdP (Authentik and
  Keycloak allow it by default) could otherwise name themselves `admin` and
  take that account over.
- Not found, auto-create off → refused.
- Linking an existing account: a signed-in user presses "Link SSO account" in
  Settings → Users (`POST /settings/sso/link`, self-service, CSRF-protected by
  the origin checks like every other POST). That starts the same flow with the
  user's id in the signed cookie; the callback requires the same session to
  still be signed in as that user and attaches `(iss, sub)` to it. The user has
  proven both identities, so nothing is guessed. An identity already linked to
  another user is refused.

SSO-created users get a bcrypt hash of 32 random bytes as their password: no
one knows it, so password login fails for them exactly like a wrong password
(same timing), and the column stays NOT NULL. An admin can still set a password
for them with the existing reset.

## Roles

With `NETIS_OIDC_ADMIN_GROUP` set, every SSO login sets the role from the
group claim: member → admin, otherwise viewer. Removing someone from the group
demotes them at their next login. Their existing sessions keep the new role
immediately, since sessions look the role up on every request; sessions are
not revoked. Demotion is not guarded against removing the last admin: the
local setup admin is the backstop, and group membership at the IdP can be
restored.

## Password login when SSO is primary

`NETIS_OIDC_DISABLE_PASSWORD=true` hides the password form (the login page
shows only the SSO button, with a small "Sign in with a local account" link to
`/login?local=1`) and makes `POST /login` refuse every account except local
admins — admins with no SSO link. That keeps a way in when the IdP is down
(break-glass) without letting SSO users or viewers fall back to passwords.
Refused logins look like a wrong password.

## Tests

A fake provider in `httptest` serves discovery, JWKS and a token endpoint that
signs ID tokens with a test RSA key and echoes the nonce it was given at
`/authorize`, checking the PKCE verifier. Cases: full flow (redirect → callback
→ working session with the right cookie flags); bad state rejected; wrong nonce
rejected; admin group → admin; no group → viewer; demotion on relogin;
auto-create off rejects an unknown identity; username clash with a local user
refused; linking from a signed-in session; disable-password refuses a viewer
but lets the local admin in; config validation. The route walk lists
`POST /settings/sso/link` as self-service.
