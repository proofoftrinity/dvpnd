// SPDX-License-Identifier: Apache-2.0

package hysteria

import (
	"strings"
)

// configTemplate is the hysteria server configuration the node writes at
// start. One QUIC listener with the node's certificate; clients are
// authenticated by an HTTP call to the node; traffic is read from the
// statistics API. Strings are JSON-quoted, which YAML accepts.
//
// The ACL applies the node's egress policy: the blocked networks (as CIDRs,
// not geoip:private, which makes hysteria download a database), localhost by
// name, and TCP port 25 unless allowed. Hysteria resolves a destination given
// as a name before it matches, so an address rule also stops a name that
// points into a blocked network. Anything no rule matches goes out direct.
var configTemplate = strings.TrimSpace(`
listen: ":{{ .Server.ListenPort }}"
tls:
  cert: {{ json .TLSCertPath }}
  key: {{ json .TLSKeyPath }}
{{- if .Server.ObfsPassword }}
obfs:
  type: salamander
  salamander:
    password: {{ json .Server.ObfsPassword }}
{{- end }}
auth:
  type: http
  http:
    url: "http://127.0.0.1:{{ .API.AuthPort }}/auth"
    insecure: false
trafficStats:
  listen: "127.0.0.1:{{ .API.StatsPort }}"
  secret: {{ json .StatsSecret }}
{{- if .Server.Up }}
bandwidth:
  up: {{ json .Server.Up }}
  down: {{ json .Server.Down }}
{{- end }}
ignoreClientBandwidth: false
disableUDP: false
acl:
  inline:
    - {{ json (printf "reject(suffix:%s)" .BlockedDomain) }}
{{- range .BlockedNetworks }}
    - {{ json (printf "reject(%s)" .) }}
{{- end }}
{{- if not .AllowSMTP }}
    - {{ json (printf "reject(all, tcp/%d)" .SMTPPort) }}
{{- end }}
`) + "\n"

// templateData is the configuration plus what only exists at runtime.
type templateData struct {
	Server interface{}
	API    interface{}

	TLSCertPath string
	TLSKeyPath  string
	StatsSecret string

	// The egress policy: see common.Egress.
	BlockedNetworks []string
	BlockedDomain   string
	SMTPPort        int
	AllowSMTP       bool
}
