"""WireGuard config / status parser.

Supports two input formats:

1. wg0.conf (wg-quick format):
   [Interface]
   Address = 10.8.0.1/24
   ...
   [Peer]
   # friendly-name (optional comment on the line before or after PublicKey)
   PublicKey = BASE64KEY=
   AllowedIPs = 10.8.0.2/32

2. `wg show <iface>` output:
   peer: BASE64KEY=
     endpoint: 1.2.3.4:51820
     allowed ips: 10.8.0.2/32
     latest handshake: 1 minute ago
"""
from __future__ import annotations

import re
from dataclasses import dataclass, field


@dataclass
class WgPeer:
    pubkey: str
    allowed_ips: list[str] = field(default_factory=list)
    name: str | None = None
    endpoint: str | None = None


def parse_wg_conf(text: str) -> list[WgPeer]:
    """Parse a wg-quick style .conf file and return one WgPeer per [Peer] block."""
    peers: list[WgPeer] = []
    current: WgPeer | None = None
    pending_comment: str | None = None

    for raw_line in text.splitlines():
        line = raw_line.strip()

        if line.startswith("[Peer]"):
            if current is not None:
                peers.append(current)
            current = WgPeer(pubkey="")
            pending_comment = None
            continue

        if line.startswith("[Interface]"):
            if current is not None:
                peers.append(current)
                current = None
            continue

        # Comments immediately before a PublicKey line carry the peer name
        if line.startswith("#"):
            pending_comment = line.lstrip("#").strip() or None
            continue

        if current is None:
            continue

        key, _, value = line.partition("=")
        key = key.strip()
        value = value.strip()

        if key == "PublicKey":
            current.pubkey = value
            if pending_comment and not current.name:
                current.name = pending_comment
            pending_comment = None
        elif key == "AllowedIPs":
            for part in value.split(","):
                part = part.strip()
                if part:
                    current.allowed_ips.append(part)
        else:
            pending_comment = None  # non-comment line resets pending name

    if current is not None and current.pubkey:
        peers.append(current)

    return [p for p in peers if p.pubkey]


def parse_wg_show(text: str) -> list[WgPeer]:
    """Parse `wg show <iface>` output and return one WgPeer per peer block."""
    peers: list[WgPeer] = []
    current: WgPeer | None = None

    for raw_line in text.splitlines():
        line = raw_line.strip()

        m = re.match(r"^peer:\s+(.+)$", line)
        if m:
            if current is not None:
                peers.append(current)
            current = WgPeer(pubkey=m.group(1).strip())
            continue

        if current is None:
            continue

        if line.startswith("allowed ips:"):
            raw = line.split(":", 1)[1].strip()
            for part in raw.split(","):
                part = part.strip()
                if part and part != "(none)":
                    current.allowed_ips.append(part)
        elif line.startswith("endpoint:"):
            current.endpoint = line.split(":", 1)[1].strip()

    if current is not None and current.pubkey:
        peers.append(current)

    return peers


def parse_wg(text: str) -> list[WgPeer]:
    """Auto-detect wg0.conf vs `wg show` output and parse accordingly."""
    stripped = text.lstrip()
    if stripped.startswith("[Interface]") or stripped.startswith("[Peer]"):
        return parse_wg_conf(text)
    return parse_wg_show(text)
