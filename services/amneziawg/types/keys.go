// SPDX-License-Identifier: Apache-2.0

package types

const (
	// Type is the numeric service type clients use for AmneziaWG nodes.
	Type = 5

	ConfigFileName = "amneziawg.toml"

	// VersionDefault and Version3 are the awg_version values of the two
	// tiers a node can offer: the default parameter set, which every client
	// engine from AmneziaWG 1.0 up accepts and current apps get without
	// asking, and the AmneziaWG 3.1 set (header protection, random
	// trailers), handed only to a client that asks for it in its peer request.
	VersionDefault = 2
	Version3       = 3

	// V3Interface is the 3.1 tier's interface; the default tier keeps awg0.
	V3Interface = "awg1"
	// V3MTU is the tunnel MTU of the 3.1 tier, which Amnezia recommends for
	// it: the prefixes, trailers and padding of every packet need the room.
	V3MTU = 1280
	// V3MinPadding is the smallest junk prefix header protection works
	// with: the first bytes of each packet carry the header cipher's nonce.
	V3MinPadding = 12
	// HeaderKeyLength is the header protection key's size in bytes.
	HeaderKeyLength = 32
)
