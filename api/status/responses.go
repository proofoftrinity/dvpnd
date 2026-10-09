// SPDX-License-Identifier: Apache-2.0
// Modified from sentinel-official/dvpn-node @ 62bde16 (2024-01-25). See NOTICE.

package status

import (
	"time"
)

type (
	// Bandwidth is the figure the node advertises, in bytes per second, and
	// Source says whether it was measured ("speedtest"), declared by the
	// operator ("config"), or is missing ("none", with zeros).
	Bandwidth struct {
		Download int64  `json:"download"`
		Upload   int64  `json:"upload"`
		Source   string `json:"source"`
	}
	Handshake struct {
		Enable bool   `json:"enable"`
		Peers  uint64 `json:"peers"`
	}
	Location struct {
		City        string  `json:"city"`
		Country     string  `json:"country"`
		CountryCode string  `json:"country_code"` // ISO 3166-1 alpha-2, empty when unknown
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
		Source      string  `json:"source"` // service that answered, "static", or "none"
	}
	QOS struct {
		MaxPeers int `json:"max_peers"`
	}
	ResponseGetStatus struct {
		Address                string        `json:"address"`
		Bandwidth              *Bandwidth    `json:"bandwidth"`
		Handshake              *Handshake    `json:"handshake"`
		IntervalSetSessions    time.Duration `json:"interval_set_sessions"`
		IntervalUpdateSessions time.Duration `json:"interval_update_sessions"`
		IntervalUpdateStatus   time.Duration `json:"interval_update_status"`
		Location               *Location     `json:"location"`
		Moniker                string        `json:"moniker"`
		Operator               string        `json:"operator"`
		Peers                  int           `json:"peers"`
		GigabytePrices         string        `json:"gigabyte_prices"`
		HourlyPrices           string        `json:"hourly_prices"`
		QOS                    *QOS          `json:"qos"`
		Type                   uint64        `json:"type"`
		Version                string        `json:"version"`
	}
)

// RootLocation is the location as the root document carries it: no source,
// which is this node's own detail and stays on /status.
type RootLocation struct {
	City        string  `json:"city"`
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
}

// RootVersion is the build the node runs, as the root document carries it.
// Name says which node software this is: several implementations speak the
// same node API, and tag and commit only mean something next to the name.
type RootVersion struct {
	Name   string `json:"name"`
	Tag    string `json:"tag"`
	Commit string `json:"commit"`
}

// ResponseGetRoot is the root document current client apps and node
// aggregators read: the node's address, measured bandwidth in bytes per
// second, whether Handshake DNS is on, location, moniker, peer count, the
// service and its public inbounds, and the version as an object. This is
// the layout nodes on the network answer with (observed from a client);
// the legacy fields stay on /status for older clients.
type ResponseGetRoot struct {
	Addr            string        `json:"addr"`
	Downlink        string        `json:"downlink"` // bytes per second, as a string like the network's nodes
	HandshakeDNS    bool          `json:"handshake_dns"`
	Location        *RootLocation `json:"location"`
	Moniker         string        `json:"moniker"`
	Peers           int           `json:"peers"`
	ServiceType     string        `json:"service_type"`
	ServiceMetadata interface{}   `json:"service_metadata"` // the service's PublicMetadata
	Uplink          string        `json:"uplink"`
	Version         *RootVersion  `json:"version"`
}
