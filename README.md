# netis

Lightweight, self-hosted **IP Address Management (IPAM)** for homelabs, makers, and small networks.

netis is not just a database — it's a **reconciliation system** between your intended
network configuration and what's actually on the wire. It answers:

- Which IPs are assigned? Which are free?
- Which devices are active? Which are unknown?
- Are there conflicts between configured and observed state?

## Stack

- **Backend:** FastAPI (Python 3.12), SQLAlchemy 2.0, Alembic, Pydantic v2, JWT auth
- **Database:** SQLite (default) or PostgreSQL
- **Frontend:** Vue 3 + TypeScript + Vite + Tailwind + Pinia + Vue Router
- **Discovery:** ARP scanning (scapy / arping), ICMP ping sweep, DHCP lease imports, Pi-hole v6 import

## Quick start (Docker)

```bash
cp .env.example .env
# Edit .env and set NETIS_SECRET_KEY to something random.

docker compose up -d --build
```

Then open [http://localhost:8080](http://localhost:8080). The first user you register
becomes the admin automatically.

To switch from SQLite to PostgreSQL:

```bash
docker compose --profile postgres up -d --build
# Then edit .env:
#   NETIS_DATABASE_URL=postgresql+psycopg://netis:netis@postgres:5432/netis
docker compose restart backend
```

## Development

### Backend

```bash
cd backend
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
cp .env.example .env

# Tests
pytest

# Dev server (creates tables automatically when using SQLite)
uvicorn app.main:app --reload
```

The API is then available at `http://localhost:8000/api/`. OpenAPI docs at
`http://localhost:8000/docs`.

#### Migrations

When using PostgreSQL or for production SQLite, manage schema via Alembic:

```bash
# Apply latest schema
alembic upgrade head

# Create a new migration after editing models
alembic revision --autogenerate -m "describe change"
```

### Frontend

```bash
cd frontend
npm install
npm run dev
```

Vite dev server runs on [http://localhost:5173](http://localhost:5173) and proxies
`/api/*` to the backend at `http://localhost:8000`.

```bash
npm run typecheck   # vue-tsc strict typecheck
npm run build       # production build → dist/
```

## How it works

### Authoritative vs observed state

This is the single most important design rule in netis: **observed network state
is never automatically merged into authoritative assignments.** Discovery and lease
imports only write to the `observations` table. The `ip_addresses` table (with
statuses `reserved`, `static`, `dhcp`) reflects what *you* decided.

The subnet view reconciles both: every host IP in the CIDR is shown with its
authoritative status and the most-recent observation, and conflicts (e.g. assigned
to device X but observed with a different MAC) are surfaced explicitly.

### Status legend

| Status     | Meaning |
| ---------- | ------- |
| `free`     | No assignment, no recent observation |
| `reserved` | Manually reserved, not necessarily live |
| `static`   | Statically assigned to a device |
| `dhcp`     | Assigned via DHCP |
| `observed` | Seen on the network but unmanaged |
| `conflict` | Assigned MAC ≠ observed MAC |

### Discovery

- **ARP scan** (primary) — fastest and most reliable on a local segment. Requires
  raw-socket privileges; runs `scapy` first, falls back to `arping`, then `arp-scan`,
  then reads the kernel ARP cache passively.
- **Ping sweep** (secondary) — reachability-only signal, no privileges needed.
- **DHCP lease import** — supports dnsmasq, ISC dhcpd, generic CSV, and JSON
  (including UniFi controller exports). Auto-detects format on upload.
- **Pi-hole v6 import** — pulls active DHCP leases and custom local DNS records directly
  from a Pi-hole v6 instance. Configure in **Settings → Pi-hole import**. Credentials are
  used only for the request and are never stored.

#### ARP scanning privileges

ARP scanning requires raw-socket access. There are three ways to grant it:

**Option 1 — `setcap` on `arping` (recommended for bare-metal / dev)**

```bash
sudo setcap cap_net_raw+ep $(which arping)
# or, if you prefer arp-scan:
sudo apt install arp-scan
sudo setcap cap_net_raw+ep $(which arp-scan)
```

The backend will then fall through to `arping` / `arp-scan` automatically and
work without any other configuration.

**Option 2 — Docker (recommended for containerised deployments)**

The `docker-compose.yml` already adds `CAP_NET_RAW` to the backend container:

```yaml
cap_add:
  - NET_RAW
```

This alone is enough for `arping` and `arp-scan` inside the container to work.
However, Docker's default bridge network is NATted, so scans will only see other
containers — **not** your LAN hosts. To scan your real network, also switch to
host networking:

```yaml
    cap_add:
      - NET_RAW
    network_mode: host   # ← uncomment in docker-compose.yml
```

With `network_mode: host` the container shares the host's network stack and ARP
scans work exactly as they would on bare metal. The `ports:` mapping is ignored
in this mode — the backend is reachable directly on port 8000.

**Option 3 — run the dev server as root (quick & dirty)**

```bash
sudo $(which uvicorn) app.main:app --reload
```

## API examples

All API examples assume `TOKEN=$(curl ... | jq -r .access_token)` after login.

```bash
# Register first admin
curl -X POST localhost:8000/api/auth/register \
  -H 'content-type: application/json' \
  -d '{"username":"admin","email":"admin@example.com","password":"changeme123"}'

# Login
curl -X POST localhost:8000/api/auth/login \
  -d 'username=admin&password=changeme123'

# Create subnet
curl -X POST localhost:8000/api/subnets \
  -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' \
  -d '{"name":"LAN","cidr":"192.168.1.0/24","gateway":"192.168.1.1"}'

# Trigger ARP scan
curl -X POST localhost:8000/api/discovery/scan \
  -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' \
  -d '{"subnet_id":1,"method":"arp"}'

# Import from Pi-hole v6
curl -X POST localhost:8000/api/discovery/pihole/import \
  -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' \
  -d '{"url":"http://192.168.1.1","password":"yourpassword","import_leases":true,"import_dns":true}'

# Export full backup
curl localhost:8000/api/backup/export -H "authorization: Bearer $TOKEN" > backup.json
```

## Project layout

```
backend/
  app/
    api/          # FastAPI routers
    auth/         # JWT + password hashing
    config/       # pydantic-settings
    database/     # SQLAlchemy session + Base
    models/       # ORM models
    schemas/      # Pydantic v2 request/response schemas
    scanners/     # ARP / ping / DHCP lease parsers / Pi-hole client
    services/     # reconciliation + observation recording
    utils/        # netaddr helpers
    main.py       # FastAPI app factory
  alembic/        # migrations
  tests/          # pytest suite

frontend/
  src/
    api/          # fetch client
    assets/       # global CSS (Tailwind)
    components/   # shared Vue components
    composables/  # API hooks
    layouts/      # AppLayout shell
    pages/        # route components
    router/       # vue-router config
    stores/       # Pinia stores (auth, theme)
    types/        # TS types
```

## Non-goals

netis is deliberately small. It does **not** ship enterprise RBAC, Kubernetes
integration, plugin systems, cloud sync, or SNMP inventory. If you need those,
this isn't the tool.

## License

MIT.
