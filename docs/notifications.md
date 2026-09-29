# Notifications

Sending new devices, outages, IP conflicts and integration errors to a webhook or an ntfy topic.

[← Back to README](../README.md)

Settings → Notifications (admins only) sends the events worth hearing about to
a generic webhook, an [ntfy](https://ntfy.sh) topic, or both:

| Kind | When |
| --- | --- |
| New devices | A scan or an integration finds a device netis has not seen before. |
| Offline / online | A device goes offline or comes back — **only** for devices you mark with **More › Turn on offline alerts** on their page. The mark is off for every device by default, so phones and laptops coming and going stay quiet. |
| IP conflicts | After a subnet sweep, an address is newly claimed by more than one interface. Announced once per conflict; one still present after a restart is announced again. |
| Scan and integration errors | A subnet sweep fails (the same error at most once an hour), an integration starts failing (once per outage), and when it works again. |
| Missing upstream | A Proxmox guest or WireGuard peer is no longer listed by its integration, and when it comes back. |

Each kind can be switched off. Events are collected for 30 seconds and sent
together, so a scan that turns up fifty devices sends one summary instead of
fifty messages. Sending never holds up a scan: events wait in a bounded queue,
and if it fills (an endpoint down for a long time during a busy period) the
overflow is dropped and logged. Each channel gets 5 seconds per attempt and
three attempts; a 4xx answer other than 429 is not retried.

The settings behind this page (`notify_*`) are listed under
[Settings](configuration.md#settings).

## Webhook

The webhook receives a JSON `POST`:

```json
{"event": "offline", "device": {"id": 3, "name": "nas"},
 "details": "nas (192.168.1.5) went offline",
 "time": "2026-09-28T10:00:00Z", "url": "https://netis.lan/devices/3"}
```

A batch has `"event": "batch"`, a summary in `details` ("12 new devices, 1 went
offline") and the individual events in `events`. `device` is `null` for events
not about one device, and `url` is empty unless a base URL is set. An optional
`Authorization` header value is sent as given.

## ntfy

ntfy gets the details as the message body, with `Title`, `Tags` (an emoji per
kind), `Priority` (4 for offline, conflicts and errors) and, with a base URL,
`Click` headers. Set the topic URL (e.g. `https://ntfy.sh/my-netis-topic` or
your own server) and, for a protected topic, an access token.

## Testing

**Send test** posts a test message with the saved settings and shows each
channel's result. The webhook header and ntfy token are never shown back in the
form; leave the field blank to keep the stored value or tick *clear* to remove
it.
