// SPDX-License-Identifier: Apache-2.0
// Modified from sentinel-official/dvpn-node @ 62bde16 (2024-01-25). See NOTICE.

package types

import (
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"gorm.io/gorm"
)

// Session is a local row for an on-chain session whose peer this node serves.
//
// Download, Upload and the duration the node reports are totals for the whole
// on-chain session, not just for this node process: the chain refuses a
// MsgUpdateSession whose bytes or duration are lower than what it already
// holds, and a session outlives a node restart (the client reconnects with the
// same session id while the VPN service's per-peer counters start again from
// zero). So admission records what the chain held at that moment in
// BaseDownload, BaseUpload and BaseDuration, and the counters the service
// reports are added on top of that.
type Session struct {
	gorm.Model
	ID           uint64 `gorm:"primaryKey;uniqueIndex:idx_sessions_id"`
	Subscription uint64 `gorm:"index:idx_sessions_subscription_address"`
	Key          string `gorm:"uniqueIndex:idx_sessions_key"`
	Address      string `gorm:"index:idx_sessions_address;index:idx_sessions_subscription_address"`
	Available    int64
	Download     int64
	Upload       int64
	BaseDownload int64
	BaseUpload   int64
	BaseDuration int64 // nanoseconds
}

// ServedBytes is what this node process has moved for the session: the
// reported totals minus what the chain already held when the peer was
// admitted.
func (s *Session) ServedBytes() int64 {
	return (s.Download - s.BaseDownload) + (s.Upload - s.BaseUpload)
}

// Duration is the session duration to report: what the chain held at
// admission plus the time this node has served the peer.
func (s *Session) Duration() time.Duration {
	return time.Duration(s.BaseDuration) + s.UpdatedAt.Sub(s.CreatedAt)
}

func (s *Session) GetAddress() sdk.AccAddress {
	if s.Address == "" {
		return nil
	}

	v, err := sdk.AccAddressFromBech32(s.Address)
	if err != nil {
		panic(err)
	}

	return v
}
