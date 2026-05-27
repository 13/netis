"""Best-effort MAC → vendor lookup.

Priority:
1. Scapy's bundled IEEE OUI database (required dependency, full coverage).
2. ``mac_vendor_lookup`` package if installed.
3. Small curated fallback table.
"""
from __future__ import annotations

import logging

log = logging.getLogger(__name__)

_scapy_db = None


def _load_scapy_db():
    global _scapy_db
    if _scapy_db is None:
        try:
            from scapy.config import conf  # noqa: PLC0415
            _scapy_db = conf.manufdb.d
        except Exception:  # noqa: BLE001
            _scapy_db = {}
    return _scapy_db


# Small curated fallback for environments where scapy is unavailable.
_CURATED: dict[str, str] = {
    "b827eb": "Raspberry Pi Foundation",
    "dca632": "Raspberry Pi Trading",
    "e45f01": "Raspberry Pi Trading",
    "28cdc1": "Raspberry Pi Trading",
    "d83add": "Raspberry Pi Trading",
    "2ccf67": "Raspberry Pi Trading",
    "fcecda": "Ubiquiti",
    "245a4c": "Ubiquiti",
    "788a20": "Ubiquiti",
    "f09fc2": "Ubiquiti",
    "687251": "Ubiquiti",
    "002722": "Ubiquiti",
    "74acb9": "Ubiquiti",
    "001540": "Intel",
    "3cfdfe": "Intel",
    "a0a8cd": "Intel",
    "001b21": "Intel",
    "000c29": "VMware",
    "005056": "VMware",
    "000569": "VMware",
    "001c14": "VMware",
    "525400": "QEMU/KVM",
    "0a0027": "VirtualBox",
    "080027": "VirtualBox (Oracle)",
    "00163e": "Xen",
    "001dd8": "Microsoft (Hyper-V)",
    "0003ff": "Microsoft",
    "bcd074": "Proxmox/QEMU",
    "f4ce46": "Hewlett Packard Enterprise",
    "ecebb8": "Hewlett Packard",
    "001b78": "Hewlett Packard",
    "3c2af4": "Brother",
    "001132": "Synology",
    "0011d8": "ASUSTek",
    "ac1f6b": "Super Micro",
    "0cc47a": "Super Micro",
    "001517": "Intel",
    "dca266": "Amazon",
    "fca667": "Amazon",
    "44650d": "Amazon",
    "ecfabc": "Espressif (ESP)",
    "240ac4": "Espressif (ESP)",
    "a4cf12": "Espressif (ESP)",
    "7cdfa1": "Espressif (ESP)",
    "30aea4": "Espressif (ESP)",
    "b4e62d": "Espressif (ESP)",
    "8c1f64": "TP-Link",
    "5091e3": "TP-Link",
    "001478": "TP-Link",
    "a42b8c": "Netgear",
    "9c3dcf": "Netgear",
    "001e2a": "Netgear",
    "001a11": "Google",
    "f4f5d8": "Google",
    "3c5ab4": "Google",
    "d8eb97": "TRENDnet",
    "001e58": "WistronNeweb",
    "001124": "Apple",
    "ac87a3": "Apple",
    "f0d1a9": "Apple",
    "a85c2c": "Apple",
    "dca904": "Apple",
    "001a2b": "Cisco",
    "00000c": "Cisco",
    "503eaa": "MikroTik",
    "4c5e0c": "MikroTik",
    "dc2c6e": "MikroTik",
    "e48d8c": "MikroTik",
    "6c3b6b": "MikroTik",
}


def _normalize_hex(mac: str) -> str:
    return "".join(c for c in mac.lower() if c in "0123456789abcdef")[:6]


def lookup_vendor(mac: str | None) -> str | None:
    if not mac:
        return None
    hex6 = _normalize_hex(mac)
    if len(hex6) < 6:
        return None

    # 1. Scapy IEEE OUI database
    try:
        oui_key = ":".join(hex6[i:i+2] for i in range(0, 6, 2)).upper()
        result = _load_scapy_db().get(oui_key)
        if result:
            return result[1] or result[0]
    except Exception:  # noqa: BLE001
        pass

    # 2. Optional mac_vendor_lookup package
    try:
        from mac_vendor_lookup import MacLookup  # type: ignore[import-not-found]
        return MacLookup().lookup(mac)
    except Exception:  # noqa: BLE001
        pass

    # 3. Curated fallback
    return _CURATED.get(hex6)
