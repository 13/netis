from app.scanners.leases import (
    parse_dnsmasq_leases,
    parse_isc_leases,
    parse_lease_csv,
    parse_lease_json,
)


def test_parse_dnsmasq():
    text = """1700000000 aa:bb:cc:dd:ee:01 192.168.1.10 mylaptop 01:aa:bb:cc:dd:ee:01
1700000000 aa:bb:cc:dd:ee:02 192.168.1.11 * 01:aa:bb:cc:dd:ee:02
"""
    leases = parse_dnsmasq_leases(text)
    assert len(leases) == 2
    assert leases[0].ip_address == "192.168.1.10"
    assert leases[0].mac_address == "aa:bb:cc:dd:ee:01"
    assert leases[0].hostname == "mylaptop"
    assert leases[1].hostname is None


def test_parse_isc():
    text = """
lease 10.0.0.5 {
  starts 1 2026/05/21 12:00:00;
  hardware ethernet aa:bb:cc:dd:ee:11;
  client-hostname "printer";
}
lease 10.0.0.6 {
  hardware ethernet AA:BB:CC:DD:EE:12;
}
"""
    leases = parse_isc_leases(text)
    assert len(leases) == 2
    assert leases[0].hostname == "printer"
    assert leases[1].mac_address == "aa:bb:cc:dd:ee:12"


def test_parse_csv():
    text = "ip,mac,hostname\n192.168.1.5,aa:bb:cc:dd:ee:05,printer\n"
    leases = parse_lease_csv(text)
    assert len(leases) == 1
    assert leases[0].hostname == "printer"


def test_parse_json_unifi():
    text = '{"data":[{"ip":"192.168.1.5","mac":"AA:BB:CC:DD:EE:05","hostname":"nas"}]}'
    leases = parse_lease_json(text)
    assert len(leases) == 1
    assert leases[0].mac_address == "aa:bb:cc:dd:ee:05"
