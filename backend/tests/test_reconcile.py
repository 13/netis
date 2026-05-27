from app.models import IgnoredHost, IPAddress, IPStatus, Observation, ObservationSource, Subnet
from app.services.reconcile import next_free_ip, subnet_rows, subnet_stats, unknown_devices
from app.services.observations import record_observation


def _make_subnet(db, cidr="192.168.50.0/29"):
    s = Subnet(name="test", cidr=cidr)
    db.add(s)
    db.commit()
    db.refresh(s)
    return s


def test_subnet_rows_all_free_initially(db_session):
    s = _make_subnet(db_session, "192.168.50.0/29")  # 6 host IPs
    rows = subnet_rows(db_session, s)
    assert len(rows) == 6
    assert all(r.status == IPStatus.free for r in rows)


def test_observation_marks_ip_observed_but_not_assigned(db_session):
    s = _make_subnet(db_session, "192.168.50.0/29")
    record_observation(
        db_session,
        ip_address="192.168.50.2",
        mac_address="aa:bb:cc:dd:ee:01",
        hostname="unknown-host",
        source=ObservationSource.arp,
    )
    db_session.commit()

    rows = {r.ip_address: r for r in subnet_rows(db_session, s)}
    assert rows["192.168.50.2"].status == IPStatus.observed
    assert rows["192.168.50.2"].observed_mac == "aa:bb:cc:dd:ee:01"
    # critical: authoritative table must remain untouched
    assert rows["192.168.50.2"].assignment_id is None


def test_next_free_ip_skips_assigned(db_session):
    s = _make_subnet(db_session, "192.168.50.0/29")
    db_session.add(
        IPAddress(ip_address="192.168.50.1", subnet_id=s.id, status=IPStatus.reserved)
    )
    db_session.commit()
    assert next_free_ip(db_session, s) == "192.168.50.2"


def test_stats_counts(db_session):
    s = _make_subnet(db_session, "192.168.50.0/29")
    db_session.add_all([
        IPAddress(ip_address="192.168.50.1", subnet_id=s.id, status=IPStatus.static),
        IPAddress(ip_address="192.168.50.2", subnet_id=s.id, status=IPStatus.reserved),
    ])
    record_observation(
        db_session,
        ip_address="192.168.50.5",
        mac_address="aa:bb:cc:dd:ee:05",
        hostname=None,
        source=ObservationSource.arp,
    )
    db_session.commit()
    stats = subnet_stats(db_session, s)
    assert stats["total_ips"] == 6
    assert stats["assigned_ips"] == 2
    assert stats["observed_ips"] == 1
    assert stats["free_ips"] == 3


def test_unknown_devices_reported(db_session):
    _make_subnet(db_session, "192.168.50.0/29")
    record_observation(
        db_session,
        ip_address="192.168.50.5",
        mac_address="aa:bb:cc:dd:ee:99",
        hostname=None,
        source=ObservationSource.arp,
    )
    db_session.commit()
    unknown = unknown_devices(db_session)
    assert len(unknown) == 1
    assert unknown[0]["mac_address"] == "aa:bb:cc:dd:ee:99"


def test_ignored_host_excluded_from_unknown(db_session):
    _make_subnet(db_session, "192.168.50.0/29")
    record_observation(
        db_session,
        ip_address="192.168.50.5",
        mac_address="aa:bb:cc:dd:ee:99",
        hostname=None,
        source=ObservationSource.arp,
    )
    db_session.add(IgnoredHost(mac_address="aa:bb:cc:dd:ee:99"))
    db_session.commit()
    assert unknown_devices(db_session) == []
