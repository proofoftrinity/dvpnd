# Regression suite

Two gates every change goes through. They state what dvpnd must keep doing; a
refactor or an update that breaks one of them is a regression, whatever else
it improves.

| Gate | Command | Needs | Time |
|---|---|---|---|
| Fast | `make check` | Go | under a minute |
| Integration | `make check-integration` | Go, Docker, the internet | about ten minutes, most of it the first image build |

`make check-all` runs both.

## When to run what

- **Every change** passes `make check` before it is committed.
- **A change to a service, the egress policy, the runtime directory or the
  proxy account, the Dockerfile, `scripts/runner.sh` or `scripts/dvpnd.service`**
  also passes `make check-integration`, as does every release.
- **A bug fix comes with a test** that fails without the fix: a unit test where
  the bug can be shown without a daemon, otherwise a case in the integration
  suite. **A new behaviour comes with the test that pins it.** The suite grows
  with every change, and a test is never weakened to let a change through: if
  the behaviour is meant to change, the test changes in the same commit, and
  the commit says why.

## Fast gate: `make check`

- `gofmt`, `go vet` (the integration package included, so it always compiles);
- every unit test with the race detector. Unit tests never touch the network,
  the chain or the host's firewall: iptables, the daemons' control APIs and
  the chain are faked;
- an SPDX header on every Go file, no dependency on `sentinel-go-sdk`, and
  `docs/provenance/verify-fork.sh`.

CI runs the same checks on every push and pull request
(`.github/workflows/ci.yml`).

## Integration gate: `make check-integration`

`test/integration/run.sh` builds the node image from this tree and runs the
Go tests in `test/integration/` (build tag `integration`) inside containers.
Two Docker networks stand for the internet (`203.0.113.0/24`) and the
provider's private network (`10.99.0.0/24`, internal); a target container
sits on both with a service on port 18080 and a mail relay on port 25. Every
protocol is driven the way the node drives it: the service writes the
daemon's configuration and starts it, a peer is added through the handshake
path, and a real client is configured from the handshake payload alone.

For every protocol, under the default policy and again with `allow_smtp`:

- the client reaches the internet, by address and by name, and the node API;
- it does not reach the private network, the host's other services on any
  of its addresses or names (loopback, `localhost`, a name the host resolves
  to loopback, the host's public address), the daemon's control API, or a mail
  relay unless mail is allowed;
- the node counts the client's traffic (what the node bills on);
- once the peer is removed the client reaches nothing;
- once the service stops, no daemon and no firewall rule is left.

The tunnel tests then take the node's chains out of the path and check that
the same client reaches the private network and the host: the blocks come from
the node, not from a network that would have dropped the traffic anyway.

| Stage | What runs | How |
|---|---|---|
| `proxies` | V2Ray, XRAY (REALITY and TLS), Hysteria2; the proxy account's kernel chain on its own; the tunnel chains in the real iptables | a container with exactly the capabilities `scripts/runner.sh` gives a proxy node (read from the runner) |
| `tunnels` | WireGuard, AmneziaWG (default and 3.1 tier), OpenVPN, each with a client in a network namespace | a privileged container, with the runner's sysctls |
| `systemd` | XRAY, Hysteria2, the OpenVPN server dropping to `dvpnd-proxy` | `scripts/dvpnd.service` as shipped, in a Debian container running systemd; a drop-in replaces only `ExecStart` |

Run one stage with `test/integration/run.sh <stage>`. The daemons log to the
output; a failing case prints its client's log.

Not covered: legacy (non-nft) iptables, IPv6 through a tunnel, the Handshake
resolver, V2Ray under the unit (the image's V2Ray is Alpine's build), and
anything on the chain. The WireGuard stage uses the host kernel's in-tree
wireguard module, which the kernel loads on demand, as it does for any
WireGuard node in Docker.

## Adding a case

- A proxy protocol: a `proxyCase` in `test/integration/proxies_test.go` (how
  the node's configuration is written, how a client is built from the
  handshake entry).
- A tunnel protocol or tier: a `tunnelCase` or `tunnelTier` in
  `test/integration/tunnels_test.go`.
- A destination clients must or must not reach: `clientView` in
  `test/integration/main_test.go`, which every protocol is checked against.
