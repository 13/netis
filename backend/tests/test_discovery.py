def _seed_observation(client, auth_headers, ip="10.0.0.50", mac="aa:bb:cc:dd:ee:77"):
    client.post(
        "/api/subnets",
        json={"name": "LAN", "cidr": "10.0.0.0/24"},
        headers=auth_headers,
    )
    res = client.post(
        "/api/discovery/leases/import",
        json=[{"ip_address": ip, "mac_address": mac, "hostname": "mystery", "source": "dhcp"}],
        headers=auth_headers,
    )
    assert res.status_code == 200, res.text


def test_ignore_and_unignore_flow(client, auth_headers):
    _seed_observation(client, auth_headers)

    # Host shows up as unknown
    unknown = client.get("/api/discovery/unknown", headers=auth_headers).json()
    assert len(unknown) == 1
    assert unknown[0]["mac_address"] == "aa:bb:cc:dd:ee:77"

    # Ignore it
    res = client.post(
        "/api/discovery/ignore",
        json={"mac_address": "aa:bb:cc:dd:ee:77"},
        headers=auth_headers,
    )
    assert res.status_code == 201, res.text
    ignored_id = res.json()["id"]

    # Now hidden from unknown, present in ignored list
    assert client.get("/api/discovery/unknown", headers=auth_headers).json() == []
    ignored = client.get("/api/discovery/ignored", headers=auth_headers).json()
    assert len(ignored) == 1

    # Un-ignore restores it
    res = client.delete(f"/api/discovery/ignored/{ignored_id}", headers=auth_headers)
    assert res.status_code == 204
    assert len(client.get("/api/discovery/unknown", headers=auth_headers).json()) == 1


def test_ignore_requires_mac_or_ip(client, auth_headers):
    res = client.post("/api/discovery/ignore", json={}, headers=auth_headers)
    assert res.status_code == 400


def test_device_list_enriched_with_last_seen(client, auth_headers):
    _seed_observation(client, auth_headers, ip="10.0.0.60", mac="aa:bb:cc:dd:ee:88")
    client.post(
        "/api/devices",
        json={"hostname": "host88", "mac_address": "aa:bb:cc:dd:ee:88", "device_type": "server"},
        headers=auth_headers,
    )
    devices = client.get("/api/devices", headers=auth_headers).json()
    host = next(d for d in devices if d["mac_address"] == "aa:bb:cc:dd:ee:88")
    assert host["primary_ip"] == "10.0.0.60"
    assert host["last_seen"] is not None
