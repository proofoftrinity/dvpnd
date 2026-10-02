// SPDX-License-Identifier: Apache-2.0

package session

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net/http"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gin-gonic/gin"
	v1base "github.com/sentinel-official/sentinelhub/v12/types/v1"
	sessiontypes "github.com/sentinel-official/sentinelhub/v12/x/session/types/v3"
	subscriptiontypes "github.com/sentinel-official/sentinelhub/v12/x/subscription/types/v3"

	"github.com/trinitystake/dvpnd/v9/context"
	"github.com/trinitystake/dvpnd/v9/types"
)

// apiError carries the HTTP status and the numeric code the response envelope
// reports. Codes are the ones upstream used, so client error handling that keys
// on them keeps working.
type apiError struct {
	Status int
	Code   int
	Err    error
}

func (e *apiError) Error() string { return e.Err.Error() }

func newAPIError(status, code int, err error) *apiError {
	return &apiError{Status: status, Code: code, Err: err}
}

// replyError answers a handshake with an error. When the node itself failed
// (a 5xx) the detail is logged and the client gets a generic message: an RPC
// endpoint's error or the proxy's is the operator's business.
func replyError(ctx *context.Context, c *gin.Context, status, code int, err error) {
	if status >= http.StatusInternalServerError {
		ctx.Log().Error("Handshake failed inside the node", "path", c.Request.URL.Path, "code", code, "error", err)
		err = errors.New(types.InternalErrorMessage)
	}
	c.JSON(status, types.NewResponseError(code, err))
}

// admitRequest is a session admission that has already been authenticated:
// AccAddress is the account proven to hold the key, ID the on-chain session and
// PeerData the service-specific peer material (WireGuard public key, or proxy
// byte + UUID for V2Ray).
type admitRequest struct {
	AccAddress sdk.AccAddress
	ID         uint64
	PeerData   []byte
}

// PeerKey is the identity the service and the local database use for the peer.
func (r admitRequest) PeerKey() string {
	return base64.StdEncoding.EncodeToString(r.PeerData)
}

type admitResult struct {
	Session sessiontypes.Session
	Peer    []byte // what Service.AddPeer returned (WireGuard: assigned v4+v6)
}

// admit checks the session on the chain, enforces the byte caps, evicts any
// earlier peer of the same account, adds the peer to the VPN service and
// records the session locally. It is shared by the legacy and the current
// handshake endpoints.
func admit(ctx *context.Context, req admitRequest) (*admitResult, *apiError) {
	if ctx.Service().PeerCount() >= ctx.Config().QOS.MaxPeers {
		return nil, newAPIError(http.StatusBadRequest, 1,
			fmt.Errorf("reached maximum peers limit %d", ctx.Config().QOS.MaxPeers))
	}

	item := types.Session{}
	ctx.Database().Model(&types.Session{}).Where(&types.Session{ID: req.ID}).First(&item)
	if item.ID != 0 {
		return nil, newAPIError(http.StatusConflict, 3,
			fmt.Errorf("session %d already exists in database", req.ID))
	}

	item = types.Session{}
	ctx.Database().Model(&types.Session{}).Where(&types.Session{Key: req.PeerKey()}).First(&item)
	if item.ID != 0 {
		return nil, newAPIError(http.StatusConflict, 3,
			fmt.Errorf("key %s for service already exists", types.KeyTag(req.PeerKey())))
	}

	session, err := ctx.Client().QuerySession(req.ID)
	if err != nil {
		return nil, newAPIError(http.StatusInternalServerError, 5, err)
	}
	if session == nil {
		return nil, newAPIError(http.StatusNotFound, 5, fmt.Errorf("session %d does not exist", req.ID))
	}
	if !session.GetStatus().Equal(v1base.StatusActive) {
		return nil, newAPIError(http.StatusNotFound, 5,
			fmt.Errorf("invalid status %s for session %d", session.GetStatus(), session.GetID()))
	}
	if session.GetAccAddress() != req.AccAddress.String() {
		return nil, newAPIError(http.StatusBadRequest, 5,
			fmt.Errorf("account address mismatch; expected %s, got %s", req.AccAddress, session.GetAccAddress()))
	}
	if session.GetNodeAddress() != ctx.Address().String() {
		return nil, newAPIError(http.StatusBadRequest, 7,
			fmt.Errorf("node address mismatch; expected %s, got %s", ctx.Address(), session.GetNodeAddress()))
	}

	// remainingBytes is the local cap enforced by the set_sessions job; 0 means
	// no cap. Every session type may carry max_bytes on the chain; plan
	// subscription sessions are additionally bounded by the account's allocation.
	var (
		remainingBytes int64  = 0
		subscriptionID uint64 = 0
	)

	if max := session.GetMaxBytes(); max.IsPositive() {
		diff := max.Sub(session.TotalBytes())
		if !diff.IsPositive() {
			return nil, newAPIError(http.StatusBadRequest, 8,
				fmt.Errorf("session %d has used its %s bytes", session.GetID(), max))
		}
		remainingBytes = clampInt64(diff)
	}

	if s, ok := session.(*subscriptiontypes.Session); ok {
		subscriptionID = s.SubscriptionID

		subscription, err := ctx.Client().QuerySubscription(s.SubscriptionID)
		if err != nil {
			return nil, newAPIError(http.StatusInternalServerError, 6, err)
		}
		if subscription == nil {
			return nil, newAPIError(http.StatusNotFound, 6,
				fmt.Errorf("subscription %d does not exist", s.SubscriptionID))
		}
		if !subscription.Status.Equal(v1base.StatusActive) {
			return nil, newAPIError(http.StatusBadRequest, 6,
				fmt.Errorf("invalid status %s for subscription %d", subscription.Status, subscription.ID))
		}

		alloc, err := ctx.Client().QueryAllocation(subscription.ID, req.AccAddress)
		if err != nil {
			return nil, newAPIError(http.StatusInternalServerError, 8, err)
		}
		if alloc == nil {
			return nil, newAPIError(http.StatusNotFound, 8,
				fmt.Errorf("allocation %d/%s does not exist", subscription.ID, req.AccAddress))
		}

		// Count bytes this node has already served on the same allocation but
		// not yet reported to the chain.
		var items []types.Session
		ctx.Database().Model(&types.Session{}).Where(&types.Session{
			Subscription: subscription.ID,
			Address:      req.AccAddress.String(),
		}).Find(&items)

		for i := 0; i < len(items); i++ {
			alloc.UtilisedBytes = alloc.UtilisedBytes.Add(sdkmath.NewInt(items[i].ServedBytes()))
		}

		if alloc.UtilisedBytes.GTE(alloc.GrantedBytes) {
			return nil, newAPIError(http.StatusBadRequest, 8,
				fmt.Errorf("invalid allocation; granted bytes %s, utilised bytes %s", alloc.GrantedBytes, alloc.UtilisedBytes))
		}

		if left := clampInt64(alloc.GrantedBytes.Sub(alloc.UtilisedBytes)); remainingBytes == 0 || left < remainingBytes {
			remainingBytes = left
		}
	}

	// One peer per account per (subscription or node) session set: drop any
	// earlier peer of this account before adding the new one.
	var items []types.Session
	ctx.Database().Model(&types.Session{}).Where(&types.Session{
		Subscription: subscriptionID,
		Address:      req.AccAddress.String(),
	}).Find(&items)

	for i := 0; i < len(items); i++ {
		if err = ctx.RemovePeerIfExists(items[i].Key); err != nil {
			return nil, newAPIError(http.StatusInternalServerError, 9, err)
		}
	}

	peer, err := ctx.Service().AddPeer(req.PeerData)
	if err != nil {
		return nil, newAPIError(http.StatusInternalServerError, 10, err)
	}
	ctx.Log().Info("Added a new peer", "key", types.KeyTag(req.PeerKey()), "count", ctx.Service().PeerCount())

	// Start the reported totals from what the chain already holds: a client
	// that reconnects after a node restart keeps its session, and the chain
	// refuses any report lower than the last one (types.Session explains).
	var (
		baseDownload = clampInt64(session.GetDownloadBytes())
		baseUpload   = clampInt64(session.GetUploadBytes())
	)

	ctx.Database().Model(&types.Session{}).Create(&types.Session{
		ID:           req.ID,
		Subscription: subscriptionID,
		Key:          req.PeerKey(),
		Address:      req.AccAddress.String(),
		Available:    remainingBytes,
		Download:     baseDownload,
		Upload:       baseUpload,
		BaseDownload: baseDownload,
		BaseUpload:   baseUpload,
		BaseDuration: int64(session.GetDuration()),
	})

	return &admitResult{Session: session, Peer: peer}, nil
}

// clampInt64 converts a chain integer to int64, saturating at MaxInt64 and
// treating a negative value as zero.
func clampInt64(v sdkmath.Int) int64 {
	if v.IsNegative() {
		return 0
	}
	if v.IsInt64() {
		return v.Int64()
	}

	return math.MaxInt64
}
