// SPDX-License-Identifier: Apache-2.0

package common

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// BlockedNetworksV4 and BlockedNetworksV6 are the destinations no client may
// reach through the node: "this network" and loopback (the host's own
// services, the proxies' control APIs among them), the private, shared and
// link-local ranges (the provider's metadata service, the networks around
// the host, other clients on the node's tunnel), benchmarking, multicast and
// the reserved space. Every service renders its rules from these two lists,
// so the protocols cannot drift apart.
var (
	BlockedNetworksV4 = []string{
		"0.0.0.0/8",
		"10.0.0.0/8",
		"100.64.0.0/10",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"172.16.0.0/12",
		"192.0.0.0/24",
		"192.168.0.0/16",
		"198.18.0.0/15",
		"224.0.0.0/4",
		"240.0.0.0/4",
	}
	BlockedNetworksV6 = []string{
		"::/128",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
		"ff00::/8",
	}
)

// BlockedNetworks is both lists, IPv4 first.
func BlockedNetworks() []string {
	return append(append([]string(nil), BlockedNetworksV4...), BlockedNetworksV6...)
}

// JSON renders v as JSON, for the templates that write the blocked networks
// into a proxy's configuration; a JSON array is also a YAML flow sequence.
func JSON(v interface{}) (string, error) {
	b, err := json.Marshal(v)

	return string(b), err
}

// SMTPPort is the mail relay port, blocked unless the operator allows it:
// a node that lets anyone send mail from its address ends up on block lists.
const SMTPPort = 25

// BlockedLocalDomain is the name blocked by domain wherever a service can
// match names: localhost and every name under it resolve to loopback.
const BlockedLocalDomain = "localhost"

// Egress is the node-wide egress policy. The start command sets it once
// before the service's Init (the proxies render it into their
// configuration) and again before Start, when the Handshake resolver's
// address is known (the tunnel services install it as firewall rules).
type Egress struct {
	// AllowSMTP lets clients open TCP connections to port 25.
	AllowSMTP bool
	// APIPort is the node API's TCP port. Tunnel clients may reach it on the
	// node's own addresses, as anyone can from outside; zero blocks it.
	APIPort uint16
	// Resolver is the Handshake resolver's address on the tunnel; tunnel
	// clients may send DNS there. Nil when the resolver is off.
	Resolver net.IP
}

var egress struct {
	sync.RWMutex
	policy Egress
}

// SetEgress replaces the node's egress policy.
func SetEgress(e Egress) {
	egress.Lock()
	defer egress.Unlock()
	egress.policy = e
}

// EgressPolicy is what SetEgress stored; the zero policy blocks SMTP and
// lets tunnel clients reach nothing on the host.
func EgressPolicy() Egress {
	egress.RLock()
	defer egress.RUnlock()

	return egress.policy
}

// ipv6Present is a directory that exists only when the kernel has IPv6; a
// variable so tests can point it elsewhere.
var ipv6Present = "/proc/sys/net/ipv6"

// TunnelEgress keeps a tunnel interface's clients off the host and the
// networks around it. It owns two chains per interface, one jumped to from
// the top of FORWARD and one from the top of INPUT, for traffic coming in on
// the interface:
//
//   - forward: drop traffic back into the tunnel (client to client), to the
//     blocked networks, and to TCP port 25 unless allowed;
//   - input: accept replies, DNS to the Handshake resolver and the node API
//     port; drop everything else, so services bound on all of the host's
//     addresses are not reachable through the tunnel.
//
// The forwarding and masquerading rules (NAT, or WireGuard's PostUp) are
// left as they are; these chains are evaluated before them. Up flushes and
// refills the chains and adds a jump only where none is, so a node that
// died without Down leaves nothing to clean up by hand.
type TunnelEgress struct {
	Interface string
}

// ForwardChain and InputChain are the chain names; iptables allows 28
// characters and an interface name has at most 15.
func (e TunnelEgress) ForwardChain() string { return "DVPND-FWD-" + e.Interface }
func (e TunnelEgress) InputChain() string   { return "DVPND-IN-" + e.Interface }

// family is one iptables binary and the rules that differ by address family.
type family struct {
	tool    string
	blocked []string
	dns     bool // the resolver is in this family
}

func (e TunnelEgress) families(policy Egress) []family {
	v4 := family{tool: "iptables", blocked: BlockedNetworksV4, dns: policy.Resolver.To4() != nil}
	families := []family{v4}
	if _, err := os.Stat(ipv6Present); err == nil {
		v6 := family{tool: "ip6tables", blocked: BlockedNetworksV6,
			dns: policy.Resolver != nil && policy.Resolver.To4() == nil}
		families = append(families, v6)
	}

	return families
}

// chainRules are the rules appended to the two chains, in order.
func (e TunnelEgress) chainRules(f family, policy Egress) [][]string {
	var (
		fwd   = e.ForwardChain()
		in    = e.InputChain()
		rules [][]string
	)

	rules = append(rules, []string{"-A", fwd, "-o", e.Interface, "-j", "DROP"})
	for _, cidr := range f.blocked {
		rules = append(rules, []string{"-A", fwd, "-d", cidr, "-j", "DROP"})
	}
	if !policy.AllowSMTP {
		rules = append(rules, []string{"-A", fwd, "-p", "tcp", "--dport", strconv.Itoa(SMTPPort), "-j", "DROP"})
	}

	rules = append(rules, []string{"-A", in, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"})
	if f.dns {
		for _, proto := range []string{"udp", "tcp"} {
			rules = append(rules, []string{"-A", in, "-d", policy.Resolver.String(), "-p", proto, "--dport", "53", "-j", "ACCEPT"})
		}
	}
	if policy.APIPort != 0 {
		rules = append(rules, []string{"-A", in, "-p", "tcp", "--dport", strconv.Itoa(int(policy.APIPort)), "-j", "ACCEPT"})
	}
	rules = append(rules, []string{"-A", in, "-j", "DROP"})

	return rules
}

// jumps are the rules that send the interface's traffic into the chains.
func (e TunnelEgress) jumps() [][]string {
	return [][]string{
		{"FORWARD", "-i", e.Interface, "-j", e.ForwardChain()},
		{"INPUT", "-i", e.Interface, "-j", e.InputChain()},
	}
}

// RunQuiet executes one rule command whose failure is an answer the caller
// handles, not a fault: a chain or a jump that is not there (yet, or any
// more). Its output is discarded so the tool's complaint does not reach the
// node's log; a variable so tests can record it.
var RunQuiet = func(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

func run(tool string, args ...string) error {
	if err := RunCommand(tool, args...); err != nil {
		return fmt.Errorf("%s %s: %w", tool, strings.Join(args, " "), err)
	}

	return nil
}

// Up installs the chains and their jumps under the current egress policy.
// The chains are filled before they are jumped to, so no packet from the
// interface passes unfiltered while the rules go in.
func (e TunnelEgress) Up() error {
	policy := EgressPolicy()
	for _, f := range e.families(policy) {
		for _, chain := range []string{e.ForwardChain(), e.InputChain()} {
			// -N fails when the chain is left from an earlier run; then it is
			// emptied instead.
			if RunQuiet(f.tool, "-N", chain) != nil {
				if err := run(f.tool, "-F", chain); err != nil {
					return err
				}
			}
		}
		for _, rule := range e.chainRules(f, policy) {
			if err := run(f.tool, rule...); err != nil {
				return err
			}
		}
		for _, jump := range e.jumps() {
			if RunQuiet(f.tool, append([]string{"-C"}, jump...)...) == nil {
				continue
			}
			if err := run(f.tool, append([]string{"-I", jump[0], "1"}, jump[1:]...)...); err != nil {
				return err
			}
		}
	}

	return nil
}

// Down removes the jumps and the chains; what is already gone is not an
// error.
func (e TunnelEgress) Down() {
	for _, f := range e.families(Egress{}) {
		for _, jump := range e.jumps() {
			_ = RunQuiet(f.tool, append([]string{"-D"}, jump...)...)
		}
		for _, chain := range []string{e.ForwardChain(), e.InputChain()} {
			_ = RunQuiet(f.tool, "-F", chain)
			_ = RunQuiet(f.tool, "-X", chain)
		}
	}
}
