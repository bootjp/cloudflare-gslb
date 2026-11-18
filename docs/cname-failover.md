# Priority-based CNAME failover

GSLB can select one healthy DNS destination by priority and update an existing
CNAME. The destination can be a conventional server hostname, a provider's load
balancer alias, or a Cloudflare Tunnel UUID hostname. DNS selection has no
connector-process dependency; the optional [Tunnel supervisor](tunnel-replica.md)
is a separate tool.

## Configuration

Start with `config.cname.json.example` and replace the API token and zone ID
placeholders. The example uses a DNS-only CNAME. Set `proxied` to `true` for a
Cloudflare-proxied record when the destination supports it.

An origin with `record_type: CNAME` requires `cname_targets`:

| Field | Meaning |
| --- | --- |
| `name` | Unique label used in diagnostics |
| `priority` | Unique priority; larger values are preferred |
| `target` | DNS hostname to write as the CNAME value |
| `probe_url` | Fixed HTTPS readiness URL for this candidate |

For example, `app.example.com` can select `primary.example.net` at priority 100
and `backup.example.org` at priority 0. Each candidate's `/healthz` URL tests that
candidate independently. A healthy primary stays selected; primary failure moves
the CNAME to the healthy backup. Recovery switches back when
`return_to_priority` is enabled. With failback disabled, the current candidate
and lower-priority candidates remain eligible. If all eligible candidates fail,
GSLB reports an error and preserves the last CNAME.

Each CNAME has a single value, so priorities must be unique. A destination may
itself represent multiple replicas; distribution within that destination belongs
to its provider or proxy. CNAME selection does not choose those replicas.

Targets must be valid DNS hostnames. IP literals, URLs, ports, duplicate targets,
and direct self-references are rejected. Hostnames are normalized to IDNA ASCII,
including case and trailing-dot normalization, before identity comparisons.
Targets cannot be mixed with IP priority lists in one origin. A/AAAA origins can
coexist with CNAME origins in the same configuration.

## Independent probes

Use `health_check.type: url`, a timeout of 1..300 seconds, and a separate
`probe_url` for each candidate. TLS verification and status 200 are required;
redirects fail. Do not override Host/SNI, append the public origin path, or probe
the currently selected public hostname. The IDNA-normalized hostname check
prevents Unicode and punycode spellings of the public name from bypassing this
rule.

The target's own hostname is a valid probe host when it has stable routing and a
matching certificate. Another fixed probe hostname can be used when the target
cannot serve HTTPS under its DNS name. Probe DNS must stay pinned to the intended
candidate, independently of the managed CNAME. GSLB validates hostname identity;
it does not prove that an indirect DNS alias chain is independent. Disable
caching at the provider for readiness paths.

Additional health-check headers can be loaded from the deployment secret store.
Do not commit authentication values. A fixed proxy 200 is not application
readiness.

## Dry run and DNS ownership

Before enabling writes, run shadow checks with `dry_run: true` or:

```sh
go run ./cmd/oneshot -config /secure/path/cname.json -dry-run
```

Dry run reads DNS and probes candidates without updating records or sending
failover notifications. It also applies to existing A/AAAA origins. An absent
CNAME is accepted only in dry run. Normal operation requires exactly one
pre-created CNAME with a configured target and the configured proxy state.
Neither proxy mode is changed implicitly during failover.

An unknown target, unexpected proxy state, or multiple records stops writes.
After probing, GSLB re-reads the CNAME and compares its record ID, target, and proxy
state with the initial snapshot. A change or failed re-read stops the update.
The update uses the original record ID and reads the result back before reporting
success. This guard is not an atomic compare-and-swap: an edit to the same record
between the final check and update can still race with GSLB. Use one writer for
each managed hostname.

## Migration and rollback

Back up the original DNS records and proxy settings. Stop the old writer for the
hostname before changing record type. Migration from A/AAAA to CNAME is an
explicit operation; the monitor never deletes A/AAAA to perform it automatically.
Test candidate failure, backup readiness, recovery, and failback before retiring
the old ingress path. DNS propagation, observation races, and existing
connections can affect the transition; zero downtime is not guaranteed.

For rollback:

1. Stop the new writer for the hostname and restore any required firewall paths.
2. Remove the migrated CNAME, or replace its record type with an original A/AAAA
   record. A CNAME cannot coexist with A/AAAA at the same hostname.
3. Restore all original records, targets, TTLs, and proxy settings, then read them
   back to verify the restored state.
4. Re-enable the old writer after verification succeeds.

Other origins can continue using their current writer throughout.

## Legacy Tunnel configuration

Existing `tunnel_groups` configurations are accepted as a compatibility input and
normalized to `cname_targets` when loaded. Keep the legacy requirements of
`proxied: true`, UUID destinations, and published HTTPS probe hostnames for that
input. Use `cname_targets` for new configurations, arbitrary DNS destinations, or
DNS-only records. Do not supply both fields on one origin.

No Tunnel management API or replica supervisor is needed for ordinary CNAME
failover. See [shared Tunnel example](tunnel-groups.md) for Tunnel-specific setup.
