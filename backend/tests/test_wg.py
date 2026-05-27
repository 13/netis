"""Tests for WireGuard parser and import endpoint."""
import io

from app.scanners.wg import parse_wg_conf, parse_wg_show, parse_wg

WG_CONF = """\
[Interface]
Address = 10.8.0.1/24
ListenPort = 51820
PrivateKey = SERVERKEY=

[Peer]
# alice
PublicKey = ALICEPUBKEY=
AllowedIPs = 10.8.0.2/32

[Peer]
PublicKey = BOBPUBKEY=
AllowedIPs = 10.8.0.3/32, fd00::3/128
"""

WG_SHOW = """\
interface: wg0
  public key: SERVERPUB=
  private key: (hidden)
  listening port: 51820

peer: ALICEPUBKEY=
  endpoint: 1.2.3.4:51820
  allowed ips: 10.8.0.2/32
  latest handshake: 1 minute, 30 seconds ago
  transfer: 1.50 MiB received, 2.30 MiB sent

peer: BOBPUBKEY=
  allowed ips: 10.8.0.3/32
"""


def test_parse_wg_conf():
    peers = parse_wg_conf(WG_CONF)
    assert len(peers) == 2
    assert peers[0].pubkey == "ALICEPUBKEY="
    assert peers[0].name == "alice"
    assert "10.8.0.2/32" in peers[0].allowed_ips
    assert peers[1].pubkey == "BOBPUBKEY="
    assert peers[1].name is None
    assert "10.8.0.3/32" in peers[1].allowed_ips
    assert "fd00::3/128" in peers[1].allowed_ips


def test_parse_wg_show():
    peers = parse_wg_show(WG_SHOW)
    assert len(peers) == 2
    assert peers[0].pubkey == "ALICEPUBKEY="
    assert peers[0].endpoint == "1.2.3.4:51820"
    assert "10.8.0.2/32" in peers[0].allowed_ips
    assert peers[1].pubkey == "BOBPUBKEY="


def test_parse_wg_autodetect():
    assert len(parse_wg(WG_CONF)) == 2
    assert len(parse_wg(WG_SHOW)) == 2


def test_wg_import_endpoint(client, auth_headers):
    # Create subnet that matches the WireGuard peers
    client.post(
        "/api/subnets",
        json={"name": "VPN", "cidr": "10.8.0.0/24"},
        headers=auth_headers,
    )

    res = client.post(
        "/api/discovery/wg/import",
        files={"file": ("wg0.conf", io.BytesIO(WG_CONF.encode()), "text/plain")},
        headers=auth_headers,
    )
    assert res.status_code == 200, res.text
    body = res.json()
    assert body["peers_found"] == 2
    assert body["devices_created"] == 2
    assert body["ips_assigned"] == 2

    # Re-import should not duplicate
    res2 = client.post(
        "/api/discovery/wg/import",
        files={"file": ("wg0.conf", io.BytesIO(WG_CONF.encode()), "text/plain")},
        headers=auth_headers,
    )
    body2 = res2.json()
    assert body2["devices_created"] == 0
    assert body2["devices_updated"] == 2


def test_wg_device_has_pubkey(client, auth_headers):
    client.post(
        "/api/subnets",
        json={"name": "VPN", "cidr": "10.8.0.0/24"},
        headers=auth_headers,
    )
    client.post(
        "/api/discovery/wg/import",
        files={"file": ("wg0.conf", io.BytesIO(WG_CONF.encode()), "text/plain")},
        headers=auth_headers,
    )
    devices = client.get("/api/devices", headers=auth_headers).json()
    wg_devices = [d for d in devices if d["wg_pubkey"]]
    assert len(wg_devices) == 2
    assert any(d["hostname"] == "alice" for d in wg_devices)
