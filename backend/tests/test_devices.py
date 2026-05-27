import io


def test_create_and_update_device(client, auth_headers):
    res = client.post(
        "/api/devices",
        json={"hostname": "nas", "mac_address": "aa:bb:cc:dd:ee:01", "device_type": "server"},
        headers=auth_headers,
    )
    assert res.status_code == 201, res.text
    did = res.json()["id"]

    res = client.patch(
        f"/api/devices/{did}",
        json={"hostname": "nas01", "notes": "primary NAS"},
        headers=auth_headers,
    )
    assert res.status_code == 200
    assert res.json()["hostname"] == "nas01"
    assert res.json()["notes"] == "primary NAS"


def test_import_devices_csv(client, auth_headers):
    csv = "hostname,mac_address,vendor,device_type,notes\nrouter,aa:bb:cc:00:00:01,Cisco,router,core\nswitch,aa:bb:cc:00:00:02,Cisco,switch,\n"
    res = client.post(
        "/api/devices/import",
        files={"file": ("devices.csv", io.BytesIO(csv.encode()), "text/csv")},
        headers=auth_headers,
    )
    assert res.status_code == 200, res.text
    body = res.json()
    assert body["created"] == 2
    assert body["skipped"] == 0

    # Second import: same MACs should be skipped
    res = client.post(
        "/api/devices/import",
        files={"file": ("devices.csv", io.BytesIO(csv.encode()), "text/csv")},
        headers=auth_headers,
    )
    body = res.json()
    assert body["created"] == 0
    assert body["skipped"] == 2


def test_import_devices_json(client, auth_headers):
    import json

    data = json.dumps([
        {"hostname": "pi1", "mac_address": "bb:cc:dd:ee:ff:01", "device_type": "iot"},
        {"hostname": "pi2"},  # no MAC → always create
    ]).encode()
    res = client.post(
        "/api/devices/import",
        files={"file": ("devices.json", io.BytesIO(data), "application/json")},
        headers=auth_headers,
    )
    assert res.status_code == 200
    assert res.json()["created"] == 2


def test_import_devices_invalid_mac_reports_error(client, auth_headers):
    csv = "hostname,mac_address\nbadhost,not-a-mac\n"
    res = client.post(
        "/api/devices/import",
        files={"file": ("devices.csv", io.BytesIO(csv.encode()), "text/csv")},
        headers=auth_headers,
    )
    body = res.json()
    assert body["created"] == 0
    assert len(body["errors"]) == 1


def test_device_ips_endpoint(client, auth_headers):
    # Create subnet
    subnet = client.post(
        "/api/subnets",
        json={"name": "LAN", "cidr": "10.0.0.0/24"},
        headers=auth_headers,
    ).json()

    # Create device
    device = client.post(
        "/api/devices",
        json={"hostname": "server1", "mac_address": "aa:bb:cc:dd:ee:11", "device_type": "server"},
        headers=auth_headers,
    ).json()

    # Assign IP to device
    client.post(
        "/api/ips",
        json={"ip_address": "10.0.0.10", "subnet_id": subnet["id"], "device_id": device["id"], "status": "static"},
        headers=auth_headers,
    )

    res = client.get(f"/api/devices/{device['id']}/ips", headers=auth_headers)
    assert res.status_code == 200
    rows = res.json()
    assert len(rows) == 1
    assert rows[0]["ip_address"] == "10.0.0.10"
    assert rows[0]["status"] == "static"
    assert rows[0]["subnet_name"] == "LAN"
