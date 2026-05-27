"""Outbound change notifications (webhook / ntfy-compatible).

Configured via NETIS_ALERT_WEBHOOK_URL. A plain JSON POST works with most
receivers (custom endpoints, Discord/Slack via proxies, ntfy with a JSON body).
"""
from __future__ import annotations

import logging

from app.config import get_settings

log = logging.getLogger(__name__)


def _post(payload: dict) -> None:
    url = get_settings().alert_webhook_url
    if not url:
        return
    try:
        import httpx

        httpx.post(url, json=payload, timeout=5.0)
    except Exception:  # noqa: BLE001 — never let alerting raise into callers
        log.warning("alert webhook POST failed for %s", url, exc_info=True)


def notify_new_unknowns(hosts: list[dict], *, subnet=None) -> None:
    if not hosts:
        return
    _post({
        "event": "new_unknown_hosts",
        "subnet": getattr(subnet, "cidr", None),
        "count": len(hosts),
        "hosts": [
            {
                "ip_address": h.get("ip_address"),
                "mac_address": h.get("mac_address"),
                "hostname": h.get("hostname"),
            }
            for h in hosts
        ],
    })
