def test_create_and_list_subnet(client, auth_headers):
    res = client.post(
        "/api/subnets",
        json={"name": "LAN", "cidr": "192.168.1.0/24", "gateway": "192.168.1.1"},
        headers=auth_headers,
    )
    assert res.status_code == 201, res.text
    sid = res.json()["id"]

    res = client.get("/api/subnets", headers=auth_headers)
    assert res.status_code == 200
    subnets = res.json()
    assert len(subnets) == 1
    assert subnets[0]["name"] == "LAN"
    # 192.168.1.0/24 has 254 host IPs
    assert subnets[0]["total_ips"] == 254
    assert subnets[0]["free_ips"] == 254

    res = client.get(f"/api/subnets/{sid}/next-free", headers=auth_headers)
    assert res.json()["ip_address"] == "192.168.1.1"


def test_invalid_cidr_rejected(client, auth_headers):
    res = client.post(
        "/api/subnets",
        json={"name": "bad", "cidr": "not-a-cidr"},
        headers=auth_headers,
    )
    assert res.status_code == 422


def test_ip_assignment_flow(client, auth_headers):
    sub = client.post(
        "/api/subnets",
        json={"name": "LAN", "cidr": "192.168.1.0/24"},
        headers=auth_headers,
    ).json()

    dev = client.post(
        "/api/devices",
        json={"hostname": "router", "mac_address": "aa:bb:cc:dd:ee:ff", "device_type": "router"},
        headers=auth_headers,
    ).json()

    res = client.post(
        "/api/ips",
        json={
            "ip_address": "192.168.1.1",
            "subnet_id": sub["id"],
            "device_id": dev["id"],
            "status": "static",
        },
        headers=auth_headers,
    )
    assert res.status_code == 201, res.text

    res = client.get(f"/api/subnets/{sub['id']}/ips", headers=auth_headers)
    rows = res.json()
    assigned = [r for r in rows if r["status"] == "static"]
    assert len(assigned) == 1
    assert assigned[0]["ip_address"] == "192.168.1.1"
    assert assigned[0]["device_id"] == dev["id"]


def test_ip_outside_subnet_rejected(client, auth_headers):
    sub = client.post(
        "/api/subnets",
        json={"name": "LAN", "cidr": "192.168.1.0/24"},
        headers=auth_headers,
    ).json()
    res = client.post(
        "/api/ips",
        json={"ip_address": "10.0.0.1", "subnet_id": sub["id"]},
        headers=auth_headers,
    )
    assert res.status_code == 400
