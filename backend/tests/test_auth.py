def test_setup_required_then_first_user_is_admin(client):
    res = client.get("/api/auth/setup-required")
    assert res.status_code == 200
    assert res.json() == {"setup_required": True}

    res = client.post(
        "/api/auth/register",
        json={"username": "alice", "email": "alice@example.com", "password": "hunter2hunter2"},
    )
    assert res.status_code == 201, res.text
    body = res.json()
    assert body["user"]["is_admin"] is True

    res = client.get("/api/auth/setup-required")
    assert res.json() == {"setup_required": False}

    res = client.post(
        "/api/auth/register",
        json={"username": "bob", "email": "bob@example.com", "password": "hunter2hunter2"},
    )
    assert res.status_code == 201
    assert res.json()["user"]["is_admin"] is False


def test_login_returns_token(client):
    client.post(
        "/api/auth/register",
        json={"username": "alice", "email": "alice@example.com", "password": "hunter2hunter2"},
    )
    res = client.post(
        "/api/auth/login",
        data={"username": "alice", "password": "hunter2hunter2"},
    )
    assert res.status_code == 200
    body = res.json()
    assert "access_token" in body
    assert body["user"]["username"] == "alice"


def test_protected_route_requires_auth(client):
    res = client.get("/api/subnets")
    assert res.status_code == 401


def test_me_endpoint(client, auth_headers):
    res = client.get("/api/auth/me", headers=auth_headers)
    assert res.status_code == 200
    assert res.json()["username"] == "admin"


def test_change_password(client, auth_headers):
    res = client.post(
        "/api/auth/change-password",
        json={"current_password": "supersecret", "new_password": "newpassword123"},
        headers=auth_headers,
    )
    assert res.status_code == 204

    # Old password no longer works
    res = client.post("/api/auth/login", data={"username": "admin", "password": "supersecret"})
    assert res.status_code == 401

    # New password works
    res = client.post("/api/auth/login", data={"username": "admin", "password": "newpassword123"})
    assert res.status_code == 200


def test_change_password_wrong_current(client, auth_headers):
    res = client.post(
        "/api/auth/change-password",
        json={"current_password": "wrongpassword", "new_password": "newpassword123"},
        headers=auth_headers,
    )
    assert res.status_code == 400
