# Optional Cloudflare Tunnel replica supervisor

`tunnel-replica` is a standalone helper for Cloudflare Tunnel connectors. It does
not select CNAME destinations, update DNS, or require a Cloudflare management API
token. The GSLB process does not launch it or depend on it. Generic CNAME failover
works without this command; use it when a Tunnel needs application-level health
checks for each connector.

## Installation and readiness

On the target Linux host, build from the repository root and install the binary
at the path used by the systemd unit:

```sh
go build -o tunnel-replica ./cmd/tunnel-replica
sudo install -o root -g root -m 0755 tunnel-replica /usr/local/bin/tunnel-replica
```

Install cloudflared separately from its official package. The installed
`tunnel-replica` binary owns a single
cloudflared child process. It starts disconnected, requires `rise` successful
local checks before starting the child, and terminates that child after `fall`
failed checks. It probes the origin directly while disconnected, so recovery
does not depend on a running public tunnel. A healthy child that crashes is
restarted on a later successful tick. A gate exit cleans up its child.

Use `deploy/replica.json.example` and `deploy/tunnel-replica@.service`. Create the
unprivileged `cloudflared` OS account, install root-owned configuration and token
files readable only by that account, and use a different loopback metrics port
for every service on a host. Validate before installing/enabling the unit:

```sh
sudo -u cloudflared /usr/local/bin/tunnel-replica -config /etc/cloudflared-replicas/app-primary.json -validate
```

The unit runs both the gate and its child in the same cgroup. Do not run a second
unmanaged cloudflared instance with the same service token on that node: it would
continue accepting traffic while the managed replica is unhealthy.

Gate probes are local, independent of the public group URL:

| Application probe | Local gate path | Group probe path |
| --- | --- | --- |
| Standard readiness | `/healthz` | `/healthz` |
| Readiness plus external dependency check | `/healthz` | `/health/dependencies` |

Origin certificates must validate for the probe URL if HTTPS is used. HTTP gates are restricted by
deployment to loopback or a private origin path, never a public unencrypted link.
The canonical Host override is intended for local HTTP virtual hosts; use a
matching URL/certificate for HTTPS.

If the dependency probe needs an authentication header, load its value from the
secret store. A cheaper readiness check can be used for the frequent local gate,
while the group probe tests dependencies and can move the whole tier out of
service. Never use a fixed proxy 200 as application readiness.


Run the supervisor on the ingress/proxy host that owns the connector, separately
from application deployment and scaling. The helper owns one connector process
and uses a local JSON file; it does not create Tunnels, application Pods, or DNS
records. See [shared Tunnel example](tunnel-groups.md) for its use alongside the
independent [CNAME selector](cname-failover.md).
