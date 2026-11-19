# Example: shared Cloudflare Tunnels as CNAME destinations

Cloudflare Tunnel is one use of [generic CNAME failover](cname-failover.md).
Use `config.tunnels.json.example`, which sets `cname_targets` to Tunnel UUID
hostnames and `proxied` to `true`. Provision Tunnels and publish their ingress
rules separately; GSLB selects DNS destinations and does not create connectors.

## Shared replicas per priority

A primary tier can use one Tunnel UUID with replicas on two ingress hosts.
A backup tier can use another UUID and another set of hosts. Healthy replicas
within a tier connect with the same token. Cloudflare chooses a replica; equal
traffic shares are not guaranteed. GSLB chooses the priority tier by changing a
single public CNAME, using the same logic as for any other DNS destination.

All replicas in a tier must serve the same application. Run the independent
[Tunnel replica supervisor](tunnel-replica.md) for each connector to disconnect
it when its application cannot serve requests. Services need separate connector
processes so failure of one application does not disconnect unrelated services.
The supervisor has its own configuration and does not update DNS.

## Fixed group probes

Each UUID needs a fixed published HTTPS probe hostname pointing to that UUID.
The managed public hostname is not an independent probe. A UUID hostname is also
not a suitable public HTTPS probe URL; use a published name with a matching edge
certificate. Restrict each probe ingress to the exact readiness path and finish
the ingress configuration with `http_status:404`. Disable caching for readiness.
Rewrite the origin Host to the canonical application host inside cloudflared.

Remotely managed replicas in a Tunnel receive the same ingress configuration.
For example, all replicas can forward to `http://127.0.0.1:8080`, with a local
proxy routing the canonical Host to the application.

A group probe samples one connected replica. It cannot prove each replica is
healthy or remove an unhealthy replica individually. Local gates independently
probe origins and disconnect their owned connectors. Origin failure can race
with disconnection, resulting in transient 502/503 responses, a group failover,
or interrupted TCP/WebSocket connections.

Validate replicas separately by stopping one connector at a time in a
maintenance window. Test origin failure while a connector is still connected,
gate disconnection, recovery, total primary-tier failure, backup readiness, and
failback. Follow the generic [migration and rollback steps](cname-failover.md#migration-and-rollback)
for DNS record-type changes and writer ownership.
