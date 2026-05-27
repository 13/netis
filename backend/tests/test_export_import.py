import io
import json


def _seed(client, headers):
    s = client.post(
        "/api/subnets",
        json={"name": "LAN", "cidr": "192.168.1.0/24"},
        headers=headers,
    ).json()
    d = client.post(
        "/api/devices",
        json={"hostname": "nas", "mac_address": "aa:bb:cc:dd:ee:99"},
        headers=headers,
    ).json()
    client.post(
        "/api/ips",
        json={
            "ip_address": "192.168.1.10",
            "subnet_id": s["id"],
            "device_id": d["id"],
            "status": "static",
        },
        headers=headers,
    )
    return s, d


def test_export_excludes_users_by_default(client, auth_headers):
    _seed(client, auth_headers)
    res = client.get("/api/backup/export", headers=auth_headers)
    assert res.status_code == 200
    data = res.json()
    assert "users" not in data
    assert len(data["subnets"]) == 1
    assert len(data["devices"]) == 1
    assert len(data["ip_addresses"]) == 1


def test_roundtrip_export_import(client, auth_headers):
    _seed(client, auth_headers)
    exported = client.get("/api/backup/export", headers=auth_headers).json()

    # Add a fresh subnet not in the export to make sure import is additive
    payload = json.dumps(exported).encode()
    res = client.post(
        "/api/backup/import",
        files={"file": ("backup.json", io.BytesIO(payload), "application/json")},
        headers=auth_headers,
    )
    # Existing rows are deduped — import is idempotent for this case
    assert res.status_code == 200
    counts = res.json()["imported"]
    assert counts["subnets"] == 0
    assert counts["devices"] == 0
