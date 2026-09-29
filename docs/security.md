# Users, sign-in and security

Roles, sessions, single sign-on through OpenID Connect, the audit log and how stored credentials are encrypted.

[← Back to README](../README.md)

- [Roles](#roles)
- [Passwords and sessions](#passwords-and-sessions)
- [Single sign-on (OIDC)](#single-sign-on-oidc)
- [Audit log](#audit-log)
- [Encrypting stored credentials](#encrypting-stored-credentials)

## Roles

Users are either `admin` or `viewer`. Viewers see the inventory, the subnets,
events and each integration's status on the dashboard, and manage their own password, API tokens and
sessions; they do not see the Admin settings area, or any of the
edit, delete, scan, Wake-on-LAN and port-scan controls. An admin can change
any user's role from Settings → Users; netis refuses to demote the last admin,
just as it refuses to delete it. A role change applies to that user's next
request, without signing them out.

Admins add and delete users, change roles and reset passwords under
Settings → Users. API tokens act with their owner's role (see
[API tokens](api.md#api-tokens)).

## Passwords and sessions

Under Settings → Account everyone can change their password (8 to 72 bytes;
changing it signs your other sessions out), link an SSO account, and see their
sessions with per-session revoke and a sign-out-everywhere-else button.

Session tokens need no key: the database only ever holds their SHA-256
digest, so a copy of it cannot be used to sign in as anyone.

> [!NOTE]
> Sessions from before netis stored them that way are dropped on upgrade, and
> everyone signs in once more.

Logins are rate limited: 5 failed attempts per minute per client address (IPv6
clients keyed by their /64) and 10 wrong passwords per 15 minutes per account,
whatever address they come from. Behind a reverse proxy, set
`NETIS_TRUSTED_PROXIES` so the limiter sees real client addresses; see
[Behind a reverse proxy](install.md#behind-a-reverse-proxy).

## Single sign-on (OIDC)

netis can take logins from an OpenID Connect provider — Authelia, Authentik,
Keycloak, Pocket ID and the like — next to (or instead of) its own passwords.
It uses the authorization code flow with PKCE and checks the ID token's
signature, issuer, audience, expiry and nonce.

<details open>
<summary>OIDC environment variables</summary>

| Variable | Default | Meaning |
| --- | --- | --- |
| `NETIS_OIDC_ISSUER` | unset | Issuer URL, exactly as the provider's discovery document spells it (trailing slash included). Unset turns SSO off. |
| `NETIS_OIDC_CLIENT_ID` | — | Client ID registered at the provider. Required. |
| `NETIS_OIDC_CLIENT_SECRET` | empty | Client secret. Leave empty for a public client. |
| `NETIS_OIDC_REDIRECT_URL` | derived | Callback URL, registered at the provider. |
| `NETIS_BASE_URL` | unset | netis's public URL; when the redirect URL is unset it is this plus `/auth/oidc/callback`. One of the two is required. |
| `NETIS_OIDC_ADMIN_GROUP` | unset | Members of this group are admins, everyone else a viewer — set on every login, so leaving the group demotes at the next sign-in. Unset means SSO leaves roles alone: new users start as viewers and an admin promotes them in Settings. |
| `NETIS_OIDC_GROUPS_CLAIM` | `groups` | ID-token claim holding the user's groups. |
| `NETIS_OIDC_AUTO_CREATE` | `true` | Create a netis user the first time an unknown SSO identity signs in. `false` refuses them; existing accounts must be linked first (below). |
| `NETIS_OIDC_DISABLE_PASSWORD` | `false` | Hide the password form. Password login then works only for local admins (see break-glass). |

</details>

An issuer without a client ID or a redirect URL stops netis at startup. The
provider does not have to be up when netis starts: discovery happens on the
first SSO login and is retried until it works.

The login page gets a **Sign in with SSO** button. An SSO user is matched by
the provider's issuer and subject (`sub`) — never by username. The first
login of an unknown identity creates a user named after its
`preferred_username` (or its email, if the provider marks it verified; otherwise its subject). If a local account already has that name the
login is refused rather than attached to it: many providers let users choose
their own username, and matching on it would let someone call themselves
`admin` and take over that account. To use SSO with an existing account, sign
in with its password and press **Link SSO account** in Settings → Account.

SSO-created users have no usable password; an admin can give them one with the
usual reset. SSO is only offered once the first admin exists: that account is
created at setup with a password and is the way back in if the provider is
down.

**Break-glass.** With `NETIS_OIDC_DISABLE_PASSWORD=true` the login page shows
only the SSO button and a small "Sign in with a local account" link
(`/login?local=1`). Password login is refused for everyone except admins that
are not linked to SSO — keep one such account (the setup admin) with a strong
password, for when the provider is unreachable.

Failed callbacks count against the same per-address limit as wrong passwords.

### Authelia

```yaml
# configuration.yml
identity_providers:
  oidc:
    clients:
      - client_id: netis
        client_name: netis
        client_secret: '$pbkdf2-sha512$...'   # authelia crypto hash generate pbkdf2
        authorization_policy: two_factor
        require_pkce: true
        pkce_challenge_method: S256
        redirect_uris:
          - https://netis.example.com/auth/oidc/callback
        scopes: [openid, profile, email, groups]
        token_endpoint_auth_method: client_secret_basic
```

```sh
NETIS_OIDC_ISSUER=https://auth.example.com
NETIS_OIDC_CLIENT_ID=netis
NETIS_OIDC_CLIENT_SECRET=the-plaintext-secret
NETIS_BASE_URL=https://netis.example.com
NETIS_OIDC_ADMIN_GROUP=admins
```

### Authentik

Create an **OAuth2/OpenID Provider** (client type *Confidential*, redirect URI
`https://netis.example.com/auth/oidc/callback`, strict) and an application
using it with slug `netis`. Authentik's default `profile` scope already carries
a `groups` claim.

```sh
NETIS_OIDC_ISSUER=https://authentik.example.com/application/o/netis/
NETIS_OIDC_CLIENT_ID=<client id from the provider>
NETIS_OIDC_CLIENT_SECRET=<client secret from the provider>
NETIS_BASE_URL=https://netis.example.com
NETIS_OIDC_ADMIN_GROUP=netis-admins
```

### Keycloak and Pocket ID

Keycloak (`https://kc.example.com/realms/<realm>`, add a *Group Membership*
mapper named `groups` with "Full group path" off) and Pocket ID (issuer is its
base URL; public clients work with the secret left empty) are configured the
same way.

## Audit log

Every state-changing request that reaches netis is recorded in an audit log,
shown to admins under Settings → **Audit log**: when, who, the action
(`device.update`, `user.role`, `login`, ...), what it acted on, the HTTP
status it was answered with and the client address (honouring
`NETIS_TRUSTED_PROXIES`). Refused attempts are recorded too — a failed login, a
viewer's 403 — so the status column tells a change from an attempt. Writes made
through the API with a personal token are attributed to the token's owner and
marked "via API token". The log can
be filtered by user and action and is paged newest first.

No password, secret or form value is ever written to it. A failed login names
the account only when it exists: an unknown username is logged blank, since it
is often a password typed into the wrong box. Page views are not logged.
Entries are kept for `audit_retention_days` (default 180) and survive the
deletion of the account that made them.

## Encrypting stored credentials

The Proxmox API token, the Pi-hole and AdGuard Home passwords, the OPNsense
API secret and the notification credentials
(webhook header, ntfy token) live in the database's `setting` table. Set
`NETIS_SECRET_KEY` and they are encrypted there with AES-256-GCM instead:

```sh
NETIS_SECRET_KEY="$(openssl rand -base64 32)" ./netis
```

Only those rows are encrypted — the rest of the table is configuration, and
stays readable in a SQL client. Values already stored in plaintext are
re-encrypted on the next start, so adding the key to an existing deployment
does not mean re-entering anything.

> [!CAUTION]
> The key never lives in the database, so keep it with (but not inside) your
> backups: a dump restored without it leaves netis unable to read those
> settings, and it says so rather than treating the credential as unset.
> Changing the key has the same effect — clear the affected settings and enter
> them again.

`netis migrate-db` copies the rows as they are, so the same key works on the
Postgres side. On a systemd install, keep the key in `/etc/netis/env` rather
than the unit file (see
[Proxmox LXC with systemd](install.md#proxmox-lxc-with-systemd)).
