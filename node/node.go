// SPDX-License-Identifier: Apache-2.0
// Modified from sentinel-official/dvpn-node @ 62bde16 (2024-01-25). See NOTICE.

package node

import (
	"fmt"
	"path"
	"runtime/debug"

	sessiontypes "github.com/sentinel-official/sentinelhub/v12/x/session/types/v3"
	subscriptiontypes "github.com/sentinel-official/sentinelhub/v12/x/subscription/types/v3"

	"github.com/trinitystake/dvpnd/v9/context"
	"github.com/trinitystake/dvpnd/v9/types"
	"github.com/trinitystake/dvpnd/v9/utils"
)

// chain is what the jobs ask of the chain and send to it; tests replace it.
type chain interface {
	QuerySession(id uint64) (sessiontypes.Session, error)
	QuerySubscription(id uint64) (*subscriptiontypes.Subscription, error)
	UpdateSessions(items ...types.Session) error
}

// contextChain is the node's own chain client behind the chain interface.
type contextChain struct{ *context.Context }

func (c contextChain) QuerySession(id uint64) (sessiontypes.Session, error) {
	return c.Client().QuerySession(id)
}

func (c contextChain) QuerySubscription(id uint64) (*subscriptiontypes.Subscription, error) {
	return c.Client().QuerySubscription(id)
}

type Node struct {
	*context.Context
	chain chain
}

func NewNode(ctx *context.Context) *Node {
	return &Node{Context: ctx, chain: contextChain{ctx}}
}

func (n *Node) Initialize() error {
	n.Log().Info("Initializing...")

	result, err := n.Client().QueryNode(n.Address())
	if err != nil {
		return err
	}

	if result == nil {
		return n.RegisterNode()
	}

	return n.UpdateNodeInfo()
}

// Start runs the jobs and serves the API until the API server fails or a job
// panics; either error ends the node through the caller, which then stops the
// VPN service.
func (n *Node) Start(home string) error {
	errCh := make(chan error, 4)

	go n.runJob("set_sessions", n.jobSetSessions, errCh)
	go n.runJob("update_sessions", n.jobUpdateSessions, errCh)
	go n.runJob("update_status", n.jobUpdateStatus, errCh)

	var (
		certFile = path.Join(home, "tls.crt")
		keyFile  = path.Join(home, "tls.key")
	)

	go func() {
		errCh <- utils.ListenAndServeTLS(
			n.ListenOn(),
			certFile,
			keyFile,
			n.Handler(),
			n.Log(),
		)
	}()

	return <-errCh
}

// runJob runs a job for the life of the node. A job logs its own failures
// and carries on; a panic is a bug, and ends the node the way a failed API
// server does, so the VPN service is still stopped on the way out.
func (n *Node) runJob(name string, job func(), errCh chan<- error) {
	defer func() {
		if r := recover(); r != nil {
			errCh <- fmt.Errorf("job %s panicked: %v\n%s", name, r, debug.Stack())
		}
	}()

	job()
}
