# JSON API, export and metrics

The JSON API and its tokens, CSV/JSON export and CSV import, search, and the Prometheus metrics endpoint.

[← Back to README](../README.md)

- [API tokens](#api-tokens)
- [Endpoints](#endpoints)
- [Examples](#examples)
- [Errors](#errors)
- [Export and CSV import](#export-and-csv-import)
- [Prometheus metrics](#prometheus-metrics)

## API tokens

The JSON API under `/api/` accepts either the session cookie the pages use or
a personal **API token**. Create one under Settings → API tokens: it is shown
once (`netis_` followed by 43 characters), only its SHA-256 digest is stored,
and it acts with your role: a viewer's token can read, an admin's can also
write. Tokens can expire (default 90 days, 0 = never), are revoked from the
same tab, and are deleted with their user. Expired tokens are removed by the
retention sweep. Admins see and can revoke everyone's tokens.

```sh
export NETIS=http://netis.lan:8080 TOKEN=netis_...
curl -H "Authorization: Bearer $TOKEN" $NETIS/api/devices
```

## Endpoints

<details open>
<summary>All endpoints</summary>

| Endpoint | Role | Does |
| --- | --- | --- |
| `GET /api/devices` | any | every device with its IPs, MACs, tags and online state; `private_mac` is true when a MAC is randomized |
| `GET /api/devices/{id}` | any | one device |
| `GET /api/subnets` | any | configured subnets, with `dhcp_start`/`dhcp_end` and `dhcp_pool_source` (`user` or the integration that read it) when a pool is set |
| `GET /api/events?limit=N` | any | recent events, newest first (default 100, max 1000) |
| `GET /api/status` | any | version, uptime, backend, device/subnet counts, integration results |
| `GET /api/search?q=` | any | up to 8 devices (name, IP, MAC, tag, vendor, model or function) and 8 subnets (name or CIDR) matching `q`; what the command palette uses |
| `GET /api/export/devices.csv` | any | inventory as CSV (one row per device; MACs, IPs, tags `;`-joined) |
| `GET /api/export/devices.json` | any | inventory as JSON, with each interface's MAC and addresses |
| `POST /api/devices` | admin | create a device |
| `PATCH /api/devices/{id}` | admin | change some of a device's fields |
| `DELETE /api/devices/{id}` | admin | delete a device |

</details>

## Examples

```sh
# Create: name and kind are required; mac, ip, subnet_id, tags, notes,
# vendor, model, function, icon and parent_device_id are optional. Without
# subnet_id the IP goes into the narrowest configured subnet that holds it.
# icon names one of the icon picker's choices (e.g. "hard-drive", listed on
# /styleguide); anything else shows the kind's default icon.
curl -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"nas","kind":"server","mac":"aa:bb:cc:00:00:01","ip":"192.168.1.20","tags":["core"]}' \
  $NETIS/api/devices

# Update only what you send; "parent_device_id": null clears the parent and
# "tags" replaces the set.
curl -X PATCH -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"notes":"rack 2","tags":["core","storage"]}' $NETIS/api/devices/42

curl -X DELETE -H "Authorization: Bearer $TOKEN" $NETIS/api/devices/42

curl -H "Authorization: Bearer $TOKEN" -o devices.csv $NETIS/api/export/devices.csv
```

## Errors

Writes take `Content-Type: application/json` and validate exactly what the
device form does. Errors come back as `{"error": "..."}`: 400 for a bad value
or unknown parent/subnet, 401 for a missing, unknown or expired token, 403 for
a viewer, 404 for no such device, 409 for a MAC that already belongs to another
device. Token requests skip the browser cross-origin checks (they carry no
cookie to forge), but 20 failed token attempts a minute from one address get
429.

## Export and CSV import

The device list exports the inventory as CSV or JSON (the same files as
`/api/export/devices.csv` and `/api/export/devices.json` above).

**CSV import** (admins, Devices > Import CSV) creates and updates devices by
MAC address. The first row names the columns: `mac` is required; `name`,
`kind`, `ip`, `tags`, `notes`, `vendor`, `model` and `function` are optional,
and anything else (such as the export's `id` or `last_seen`) is ignored, so an
export can be edited and imported as it is. A preview lists what each line
will create, update or skip before anything is written. A new MAC creates a
manual device (it needs a name, and an IP must fall in a configured subnet).
A known MAC only fills in what is missing: notes, vendor, model and function
where empty, and tags are added, never removed. Names and kinds change only on
unreviewed scan discoveries, so devices from integrations and devices you have
reviewed keep theirs. Uploads may be up to 5 MB.

## Prometheus metrics

`GET /metrics` serves the Prometheus text format — device and subnet counts,
how many devices are online, when each integration last ran and whether it
worked, and — with [scheduled backups](backups.md#scheduled-backups) on —
`netis_last_backup_timestamp_seconds` and `netis_last_backup_success`. It needs a session too, unless you set a scrape token:

```sh
NETIS_METRICS_TOKEN="$(openssl rand -hex 16)" ./netis
```

```yaml
scrape_configs:
  - job_name: netis
    static_configs: [{targets: ['netis.lan:8080']}]
    authorization:
      credentials: <the token>
```

The token is accepted on `/metrics` only — it is a scrape credential, not a
login — and only in the `Authorization` header, so it stays out of access logs.
Without it the endpoint is not left open: the metrics name every subnet and
count every device, which is not something to publish to whoever can reach the
port.
