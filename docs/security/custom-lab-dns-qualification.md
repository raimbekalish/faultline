# Custom-lab DNS review and required qualification — M8B3A

**Source analysis only. Every runtime probe below is UNVERIFIED.** No Docker
resources were executed to resolve this question; no command/profile correction
is justified by the reviewed evidence. The fixed profile retains
`--dns=127.0.0.1`, `--dns-search=.`, internal isolated networks and exact aliases.

## What the prepared DNS option means

Docker's [DNS documentation](https://docs.docker.com/engine/network/#dns-services)
says user-defined networks use embedded DNS at `127.0.0.11`; a supplied loopback
DNS address refers to the container's namespace. It does not follow that
`--dns=127.0.0.1` replaces embedded service discovery with a nonexistent resolver.

Reviewed Moby source at commit
`9f07d9222f4277b9a46bda82b3db99186ee595ef` (2026-10-28):

- [sandbox DNS setup](https://github.com/moby/moby/blob/9f07d9222f4277b9a46bda82b3db99186ee595ef/daemon/libnetwork/sandbox_dns_unix.go)
  starts the embedded resolver and rebuilds container DNS with that resolver.
- [TransformForIntNS](https://github.com/moby/moby/blob/9f07d9222f4277b9a46bda82b3db99186ee595ef/daemon/libnetwork/internal/resolvconf/resolvconf.go)
  retains configured overrides as upstreams while replacing the resolver list
  with the internal nameserver. Overrides are dialed in the container namespace.
- [Resolver](https://github.com/moby/moby/blob/9f07d9222f4277b9a46bda82b3db99186ee595ef/daemon/libnetwork/resolver.go)
  answers local records before forwarding. Forwarding restrictions depend on
  network access and upstream origin; internal-network mode is not equivalent
  to disabling every container-namespace upstream query.

**Inference:** this implementation supports internal `toxiproxy` alias resolution
while the explicit upstream points to container loopback. This is not proof of
behavior in the owner's installed Engine/Desktop build, a DNS leak prevention
claim or permission to weaken the observer profile. No DNS listener is intended
at that upstream; an application controlling its namespace could provide one.
Even resolver failure says nothing about direct-IP routing, host reachability or
control API binding. Do not use name-resolution failure as an isolation test.

## Concrete future qualification matrix

Before these tests: separate owner authorization, reviewed real transport and
continuity/reconciliation, dedicated reviewed image builds and exact ID/profile
inspection. Use only new owned sentinels/probes and exact known owned addresses;
never scan user services, public DNS servers, unrelated networks or host ports.
Do not publish a control endpoint. Retain bounded outcomes/counters, not query
payloads, ambient resolver addresses, raw host configuration or private IPs publicly.

| Boundary | Positive control and negative probe | Required evidence / failure action |
| --- | --- | --- |
| Internal aliases | Runner resolves frontend on edge and control alias on control; application resolves toxiproxy on data; proxy resolves fixture on backend. Test A/AAAA and TCP/UDP DNS with exact aliases. | Inspect intended resolver/namespace privately; correlate answers with exact owned endpoints, then successful bounded contract/control requests. No missing/extra attachments. Any mismatch refuses startup. |
| External forwarding | Bounded random names under `.invalid`, multiple query types and resolver protocols; a later authorized test uses only an owned DNS sink to detect unintended host/upstream forwarding. Include an application-namespace loopback DNS listener adversary. | No query reaches an external/host sink; distinguish NXDOMAIN/SERVFAIL/timeout from actual no-forwarding evidence using counters. No public DNS traffic. Unexpected forwarding refuses profile. |
| Backend bypass | Application→proxy→fixture succeeds with fixture counters. Application probe to exact owned fixture backend IP/alias fails, including direct-IP and IPv6 attempts. | Counter proves proxy-path use and forbidden path receives zero requests. DNS failure alone insufficient. No arbitrary address probing. |
| Control isolation | Runner control request succeeds; app and fixture probes to exact owned control IP and proxy's other interfaces fail. | Inspect actual listener binding and use positive/negative sentinels. Multiple/wildcard bindings, alternate aliases or cross-network attachments refuse. |
| Host reachability | After explicit scoped permission, owned host/bridge sentinels provide a positive reachability control from an authorized probe, while app/fixture paths must fail. | No traffic reaches the forbidden sentinel; test literal gateway/host paths and IPv6 on the exact owned test addresses. Never probe unrelated host services. |
| Runtime/topology change | Separately authorized restart/disconnect/foreign-attachment scenarios invalidate the saved binding/profile. | New execution fails closed; unresolved resources remain reserved. Do not infer absence or clean by prefix. Repeat qualification only on a fresh approved runtime/plan. |

The current read-only observer does not expose arbitrary addresses or implement
these probes. Needed private probe metadata/commands require review; this table is
not an existing executable tool. Rootless/Desktop/backends, resolver rewrites,
connection ordering and DNS caches may differ from reviewed upstream code.

## Remaining uncertainty

The historical Desktop bridge inconsistency and exact attribution remain UNKNOWN;
this milestone made no fresh runtime inventory or restart/event query. Snapshot
equality cannot prove continuity. Local httptest timing results prove neither DNS
semantics nor network isolation. Maintain the fail-closed no-provider boundary
until a reviewed qualification/continuity design and actual tests justify execution.
