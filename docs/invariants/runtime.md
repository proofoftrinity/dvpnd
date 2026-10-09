# Runtime: how the node runs the daemons, and what it leaves behind

The protocol daemons parse hostile traffic, so they're not trusted with the host, and
the node must not leave a tunnel, a rule or a process behind when it stops. IDs as in
`test/README.md`.

- **[RT-1] The protocol daemons run as the unprivileged `dvpnd-proxy` account.** When
  the node runs as root and the account exists, the daemons start as it, with their
  files in the runtime directory and no access to the node's home or keys; OpenVPN drops
  to it once its tunnel is up.
- **[RT-2] However the node stops after its service has started, the service is stopped
  first.** A stop signal, a job that panics, or a step that fails after `Start` all take
  the tunnel down, remove the firewall rules and stop the daemon, so none of it outlives
  the node.
- **[RT-3] A daemon is stopped with SIGTERM and killed if it doesn't exit in time; a
  daemon that exits on its own is reaped.**
- **[RT-4] IP forwarding is switched on from the node, read before it is written.**
  `/proc/sys` is read-only in an unprivileged container, where the runner sets the sysctl
  instead; reading first lets that node start.
- **[RT-5] A call to a proxy's control API is bounded in time, and waits for a proxy
  that is still starting.** A proxy that stops answering must not hold a handshake or a
  job forever; a handshake right after the node starts must not fail because the
  proxy's API isn't up yet.
- **[RT-6] A service refuses to initialise without its daemon's binary.** The operator
  learns at `init`, not from a node that starts and serves nothing.
- **[RT-7] Once the service stops, no daemon and no firewall rule of it is left.**
  Checked end to end with the real daemons, under the systemd unit as shipped too.
- **[RT-8] WireGuard accepts the replies coming back into the tunnel whatever the host's
  FORWARD policy is.** On a host where it's DROP (ufw, or Docker installed) every reply
  to a peer was dropped and TCP through the tunnel hung.
- **[RT-9] A configuration file written by an earlier release still starts the node.**
  Files without `[egress]`, without V2Ray's `[api]`, without AmneziaWG's `[v3]` or with
  OpenVPN's old management port read as before; an upgrade must not need a hand edit.
