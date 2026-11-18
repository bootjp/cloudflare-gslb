# Priority groups backed by shared tunnels

This mode uses one Tunnel UUID per service and priority tier. Healthy replicas in
the same tier connect using the same token. Cloudflare chooses the replica; there
is no guarantee of equal traffic shares. GSLB selects the tier by updating a
single proxied CNAME.

A primary tier can have replicas on two hosts, while a backup tier uses another
set of hosts. All replicas in a tier must be able to serve the same application.
Services need separate connector processes so that an unhealthy replica of one
application can disconnect without also disconnecting other applications.

## Configuration and probes

Start from `config.tunnels.json.example`. The example UUIDs and secret markers are
placeholders and must be replaced before live use. Keep credentials in the
deployment secret store, never in Git. Existing A/AAAA origins can remain in the same
configuration. `dry_run` applies to every origin, including A/AAAA.

Each group has a fixed HTTPS published probe hostname pointing to its UUID. The
main public hostname is not a group probe, and the UUID hostname is not a public
HTTPS probe URL. TLS verification and status 200 are required; redirects fail.
Disable caching on the published probe paths. Allow only the exact health path on
each probe hostname and end the ingress configuration with `http_status:404`.
Rewrite the probe Host to the canonical application host inside cloudflared.

Remotely managed replicas in one Tunnel receive the same ingress configuration.
For example, replicas can use `http://127.0.0.1:8080` on each host, where a local
proxy routes the canonical Host to the application.

The group check samples a connected replica. It cannot individually select or
remove a replica. The independent local health gate is required for each replica.
An origin failure can race with disconnection and produce transient 502/503s or
a temporary group failover. Existing TCP/WebSocket connections can be interrupted.
This mode does not promise zero downtime or eliminate observation/TOCTOU races.

## Replica health gate

Build `go build -o tunnel-replica ./cmd/tunnel-replica` and install cloudflared
separately from its official package. The `tunnel-replica` binary owns a single
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
tunnel-replica -config /etc/cloudflared-replicas/app-primary.json -validate
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

## Migration and drift handling

Run shadow probes while public DNS is still A/AAAA:

```sh
go run ./cmd/oneshot -config /secure/path/tunnels.json -dry-run
```

Dry run reads DNS and probes the groups without creating records or sending
failover notifications. An absent CNAME is allowed only in dry run. Normal
operation requires one pre-created, proxied CNAME pointing to a configured UUID.
An unknown target, unexpected proxy state, or multiple records stops writes.

Back up every original DNS record and all GSLB/proxy settings. Stop the old writer
for each migrated hostname before changing record type. A/AAAA to CNAME is an
explicit migration; the monitor never deletes A/AAAA records to perform it.
Updating an existing CNAME uses its existing record ID (not create-before-delete)
and verifies the selected value by reading it back before announcing success.
All unhealthy groups leave the last CNAME unchanged and report an error.
Return to a higher tier obeys `return_to_priority`; disabling it keeps monitoring
the current/lower eligible tiers without automatically failing back.

Validate the primary replicas separately by temporarily stopping one connector
at a time in a maintenance window. A public group check alone cannot prove both
replicas work. Test app failure with cloudflared initially still connected, gate
disconnection, recovery, total primary-tier failure, backup readiness, and failback. Keep
the old ingress/firewall path until those checks pass.

For rollback, stop the new writer first, restore closed firewall paths, restore
all original A/AAAA records, then re-enable the old writer. Existing non-migrated
origins remain managed by the original deployment throughout.
