// SPDX-License-Identifier: Apache-2.0
// Modified from sentinel-official/dvpn-node @ 62bde16 (2024-01-25). See NOTICE.

package types

type GeoIPLocation struct {
	City        string  `json:"city"`
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"` // ISO 3166-1 alpha-2, empty when unknown
	IP          string  `json:"ip"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Source      string  `json:"source"` // service that answered, "static", or "none"
}
