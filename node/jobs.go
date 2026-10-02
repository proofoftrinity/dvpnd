// SPDX-License-Identifier: Apache-2.0
// Modified from sentinel-official/dvpn-node @ 62bde16 (2024-01-25). See NOTICE.

package node

import (
	"time"

	sdkmath "cosmossdk.io/math"
	v1base "github.com/sentinel-official/sentinelhub/v12/types/v1"
	subscriptiontypes "github.com/sentinel-official/sentinelhub/v12/x/subscription/types/v3"

	"github.com/trinitystake/dvpnd/v9/types"
)

func (n *Node) jobSetSessions() error {
	n.Log().Info("Starting a job", "name", "set_sessions", "interval", n.IntervalSetSessions())

	t := time.NewTicker(n.IntervalSetSessions())
	for ; ; <-t.C {
		peers, err := n.Service().Peers()
		if err != nil {
			return err
		}

		count := len(peers)
		n.Log().Debug("Validating the peers", "count", count)

		for i := 0; i < count; i++ {
			var item types.Session
			n.Database().Model(
				&types.Session{},
			).Where(
				&types.Session{
					Key: peers[i].Key,
				},
			).First(&item)

			if item.ID == 0 {
				n.Log().Info("Unknown connected peer", "key", types.KeyTag(peers[i].Key))
				if err = n.RemovePeer(peers[i].Key); err != nil {
					return err
				}

				continue
			}
			upload, download, moved := reportedUsage(item, peers[i])
			if !moved {
				n.Log().Debug("The peer has not sent any data", "key", types.KeyTag(item.Key),
					"update_at", item.UpdatedAt)
				continue
			}

			n.Database().Model(
				&types.Session{},
			).Where(
				&types.Session{
					ID: item.ID,
				},
			).Updates(
				map[string]interface{}{
					"upload":   upload,
					"download": download,
				},
			)

			var (
				available = sdkmath.NewInt(item.Available)
				consumed  = sdkmath.NewInt(peers[i].Upload + peers[i].Download)
			)

			if available.IsPositive() && consumed.GT(available) {
				n.Log().Info("Peer allocation exceeded", "key", types.KeyTag(item.Key))
				if err = n.RemovePeer(item.Key); err != nil {
					return err
				}
			}
		}
	}
}

// reportedUsage turns the service's per-peer counters, which count from the
// moment the peer was added, into the session totals to store and report:
// what the chain held when the peer was admitted plus what moved since. moved
// is false when the peer has not sent anything since the last pass.
func reportedUsage(item types.Session, peer types.Peer) (upload, download int64, moved bool) {
	upload = item.BaseUpload + peer.Upload
	download = item.BaseDownload + peer.Download

	return upload, download, upload != item.Upload
}

// jobUpdateStatus keeps the node marked active on-chain. It runs once
// immediately: a freshly registered node is inactive until its first status
// update, and the chain deactivates a node whose status is older than its
// status_timeout parameter.
func (n *Node) jobUpdateStatus() error {
	n.Log().Info("Starting a job", "name", "update_status", "interval", n.IntervalUpdateStatus())

	t := time.NewTicker(n.IntervalUpdateStatus())
	for {
		if err := n.UpdateNodeStatus(); err != nil {
			return err
		}

		<-t.C
	}
}

// jobUpdateSessions reconciles the local session table with the chain (v3):
// sessions the chain has dropped or deactivated lose their peer; sessions that
// moved data since the last pass are reported with MsgUpdateSession. A session
// that has not moved data is left alone and the chain expires it after its
// status_timeout, which is the protocol's idle timeout.
func (n *Node) jobUpdateSessions() error {
	n.Log().Info("Starting a job", "name", "update_sessions", "interval", n.IntervalUpdateSessions())

	t := time.NewTicker(n.IntervalUpdateSessions())
	for ; ; <-t.C {
		var items []types.Session
		n.Database().Model(
			&types.Session{},
		).Find(&items)

		count := len(items)
		n.Log().Info("Validating the sessions", "count", count)

		for i := count - 1; i >= 0; i-- {
			var (
				removePeer    = false
				removeSession = false
				skipUpdate    = false
			)

			session, err := n.Client().QuerySession(items[i].ID)
			if err != nil {
				return err
			}

			if session == nil {
				n.Log().Info("Session no longer exists on the chain", "key", types.KeyTag(items[i].Key), "id", items[i].ID)
				removePeer, removeSession, skipUpdate = true, true, true
			} else {
				if items[i].Upload == session.GetUploadBytes().Int64() &&
					items[i].Download == session.GetDownloadBytes().Int64() {
					skipUpdate = true
					if items[i].CreatedAt.Before(session.GetStatusAt()) {
						removePeer = true
					}

					n.Log().Info("Stale peer connection", "key", types.KeyTag(items[i].Key),
						"created_at", items[i].CreatedAt, "status_at", session.GetStatusAt())
				}
				if !session.GetStatus().Equal(v1base.StatusActive) {
					removePeer = true
					if session.GetStatus().Equal(v1base.StatusInactive) {
						removeSession, skipUpdate = true, true
					}

					n.Log().Info("Invalid session status", "key", types.KeyTag(items[i].Key),
						"id", session.GetID(), "status", session.GetStatus())
				}
				if max := session.GetMaxBytes(); max.IsPositive() {
					used := sdkmath.NewInt(items[i].Upload + items[i].Download)
					if used.GTE(max) {
						removePeer = true
						n.Log().Info("Session byte limit reached", "key", types.KeyTag(items[i].Key),
							"id", session.GetID(), "max_bytes", max, "used", used)
					}
				}
				if s, ok := session.(*subscriptiontypes.Session); ok {
					subscription, err := n.Client().QuerySubscription(s.SubscriptionID)
					if err != nil {
						return err
					}
					if subscription == nil || !subscription.Status.Equal(v1base.StatusActive) {
						removePeer = true
						if subscription == nil || subscription.Status.Equal(v1base.StatusInactive) {
							removeSession, skipUpdate = true, true
						}

						n.Log().Info("Invalid subscription status", "key", types.KeyTag(items[i].Key),
							"id", s.SubscriptionID)
					}
				}
			}

			if removePeer {
				if err = n.RemovePeerIfExists(items[i].Key); err != nil {
					return err
				}
			}

			if removeSession {
				n.Database().Model(
					&types.Session{},
				).Where(
					&types.Session{
						ID: items[i].ID,
					},
				).Unscoped().Delete(
					&types.Session{},
				)
			}

			if skipUpdate {
				items = append(items[:i], items[i+1:]...)
			}
		}

		if len(items) == 0 {
			continue
		}
		if err := n.UpdateSessions(items...); err != nil {
			return err
		}
	}
}
