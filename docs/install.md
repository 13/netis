# Installing netis

How to run netis with Docker, as a release binary, from source or in a Proxmox LXC, and how to put it behind a reverse proxy.

[← Back to README](../README.md)

- [Docker](#docker)
- [Release binary](#release-binary)
- [From source](#from-source)
- [Proxmox LXC with systemd](#proxmox-lxc-with-systemd)
- [First run](#first-run)
- [Behind a reverse proxy](#behind-a-reverse-proxy)

## Docker

Released images are published to GHCR on every `v*` tag, for `linux/amd64`:

```sh
docker pull ghcr.io/13/netis:latest
docker run -d --name netis \
  --network host \
  -v netis-data:/data \
  -e NETIS_DB=/data/netis.db \
  ghcr.io/13/netis:latest
```

Or build the image locally:

```sh
docker build -t netis .
docker run -d --name netis \
  --network host \
  -v netis-data:/data \
  -e NETIS_DB=/data/netis.db \
  netis
```

> [!IMPORTANT]
> `--network host` is required: netis discovers MAC addresses by reading the
> host's ARP table (`/proc/net/arp`) after pinging hosts on the subnet, which
> only works if the container shares the host's network namespace. Running
> netis on a bridged/NAT network will still ping and track online/offline
> state, but MAC address (and therefore vendor) discovery will not work for
> those subnets. Host networking also lets netis hear the mDNS and UPnP
> answers it uses to fill in device details.

The image runs netis as root and sets `NETIS_PRIVILEGED_ICMP=1`, so it sends
raw ICMP echo requests. Docker grants root in a container `CAP_NET_RAW` by
default, so no `--cap-add` is needed. A runtime that drops it (Podman's
defaults, `--cap-drop=ALL`, some hardened setups) needs `--cap-add=NET_RAW`;
without it the sweeps fail and show up as scan errors.

If most probes in a sweep cannot be sent at all (for example the process
lacks permission to open ICMP sockets), the sweep is reported as a scan
error on the dashboard and in Events, and device states are left as they
were rather than counted as misses.

The image has no shell; to run `netis backup` and other subcommands, use
`docker exec netis /netis ...` (see [Backups](backups.md)).

## Release binary

Every `v*` tag publishes a static Linux amd64 binary to the GitHub release,
alongside a `SHA256SUMS` file. Releases are amd64 only; on another architecture,
build from source (see [From source](#from-source)).

```sh
tar -xzf netis_<version>_linux_amd64.tar.gz
./netis
```

To send raw ICMP echo requests without running as root, give the binary
`CAP_NET_RAW` and set `NETIS_PRIVILEGED_ICMP=1`; without it netis uses the
unprivileged UDP-ICMP fallback:

```sh
sudo setcap cap_net_raw+ep ./netis
NETIS_DB=/var/lib/netis/netis.db NETIS_PRIVILEGED_ICMP=1 ./netis
```

For a long-running install, use the systemd unit below instead.

## From source

Build:

```sh
go tool templ generate && CGO_ENABLED=0 go build -o netis ./cmd/netis
```

(`make build` does the same; templ is pinned in `go.mod` as a tool, so nothing
needs installing first. See [Development](development.md).)

Run:

```sh
NETIS_DB=/var/lib/netis/netis.db ./netis
```

The server listens on `:8080` by default (`NETIS_ADDR`, see
[Configuration](configuration.md)).

## Proxmox LXC with systemd

1. Build the static binary as above (or download a prebuilt one) and copy
   it to the LXC as `/opt/netis/netis`.
2. Create a dedicated system user: `useradd -r -s /usr/sbin/nologin netis`.
3. Copy `deploy/netis.service` to `/etc/systemd/system/netis.service`.
4. `systemctl daemon-reload && systemctl enable --now netis`.

The unit sets `AmbientCapabilities=CAP_NET_RAW` so netis can send
privileged ICMP echo requests without running as root, and uses
`StateDirectory=netis` so `/var/lib/netis` exists and is writable by the
`netis` user for the SQLite database.

Secrets such as `NETIS_SECRET_KEY` belong in `/etc/netis/env` (read through
`EnvironmentFile=`, optional), not in the unit file, which any local user can
read:

```sh
install -d -m 0755 /etc/netis
install -m 0600 /dev/null /etc/netis/env
echo "NETIS_SECRET_KEY=$(openssl rand -base64 32)" >> /etc/netis/env
```

### Sandboxing

The unit is sandboxed: the filesystem is read-only except `/var/lib/netis`,
`/home` and `/root` are hidden, the capability set is limited to
`CAP_NET_RAW`, and system calls, address families and namespaces are
restricted (`systemd-analyze security netis` shows the details).

> [!WARNING]
> Because of `ProtectHome=yes`, put the WireGuard SSH key and `known_hosts`
> file under `/etc/netis` or `/var/lib/netis`, readable by the `netis` user, and
> point the `wg_ssh_key_path` / `wg_ssh_known_hosts` settings there; a key in
> `/root/.ssh` is invisible to the service. If a key must stay where it is, add
> it with a drop-in (`systemctl edit netis`) containing
> `BindReadOnlyPaths=/home/you/.ssh/netis_wg:/etc/netis/wg_key`.

Because the LXC shares the Proxmox host's bridge, it sees the same L2
segment as everything else on the LAN, so ARP-based MAC discovery works
without any special networking configuration (unlike the Docker bridged
case above).

## First run

On first run, visit `/setup` in a browser. A short setup flow follows: create
the admin account (you stay signed in), pick the subnets to scan (the ones this
machine is on are found and preselected; add others such as `192.168.1.0/24` by
hand), optionally connect Pi-hole, Proxmox and the other integrations, and the
last step starts the first scan and counts devices as they turn up.

**Skip for now** opens the app straight away; the dashboard then shows a
Finish setup panel listing what is still missing (no subnets, no scan
yet, no integrations) until it is done or dismissed. Afterwards subnets
live under **Settings → Network**, and the background scan loop sweeps
them every 120s by default.

## Behind a reverse proxy

netis ignores `X-Forwarded-For` and `X-Forwarded-Proto` unless you name the
proxy that sends them:

```sh
NETIS_TRUSTED_PROXIES=10.0.0.0/8,192.168.1.5 ./netis
```

Set this when netis sits behind nginx, Caddy, Traefik or similar. Without it
every request is attributed to the proxy's own address, so the login rate
limiter (5 failed attempts per minute) counts all users as one client and five
wrong passwords from anywhere lock everyone out for a minute. With it, the
limiter keys on the real client address — the rightmost forwarded entry that
isn't itself a listed proxy, which is the furthest-left address the proxy chain
can actually vouch for.

`X-Forwarded-Proto: https` from a listed proxy also lets the session cookie
carry the `Secure` flag when TLS terminates at the proxy.

A malformed entry is a startup error rather than a warning: a list that quietly
parsed to nothing would leave the limiter mis-keyed with no sign of it.

The per-address limiter keys IPv6 clients by their /64. Separately, each
account allows 10 wrong passwords per 15 minutes, whatever address they come
from, so rotating addresses does not buy unlimited guesses, and the
current-password check when changing your own password has the same limit. It is a
sliding window, not a lockout: the account opens up again once old failures age
out.
