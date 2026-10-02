// SPDX-License-Identifier: Apache-2.0

package v2ray

import (
	"context"
	"encoding/base64"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	proxymancommand "github.com/v2fly/v2ray-core/v5/app/proxyman/command"
	statscommand "github.com/v2fly/v2ray-core/v5/app/stats/command"
	"github.com/v2fly/v2ray-core/v5/common/serial"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v2raytypes "github.com/trinitystake/dvpnd/v9/services/v2ray/types"
)

// fakeAPI stands in for v2ray's gRPC control API: it records the users on
// the VMess inbound and answers traffic queries from a table. stall makes
// every call hang, as a proxy that stopped answering would.
type fakeAPI struct {
	proxymancommand.UnimplementedHandlerServiceServer
	statscommand.UnimplementedStatsServiceServer

	mu    sync.Mutex
	users map[string]bool
	stats map[string]int64
	stall bool
}

func (f *fakeAPI) wait(ctx context.Context) {
	f.mu.Lock()
	stall := f.stall
	f.mu.Unlock()
	if stall {
		<-ctx.Done()
	}
}

func (f *fakeAPI) AlterInbound(ctx context.Context, req *proxymancommand.AlterInboundRequest) (*proxymancommand.AlterInboundResponse, error) {
	f.wait(ctx)
	if req.GetTag() != "vmess" {
		return nil, status.Errorf(codes.Unknown, "unknown inbound %q", req.GetTag())
	}
	op, err := serial.GetInstanceOf(req.GetOperation())
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	switch op := op.(type) {
	case *proxymancommand.AddUserOperation:
		f.users[op.GetUser().GetEmail()] = true
	case *proxymancommand.RemoveUserOperation:
		if !f.users[op.GetEmail()] {
			return nil, status.Errorf(codes.Unknown, "user %q not found", op.GetEmail())
		}
		delete(f.users, op.GetEmail())
	}

	return &proxymancommand.AlterInboundResponse{}, nil
}

func (f *fakeAPI) GetStats(ctx context.Context, req *statscommand.GetStatsRequest) (*statscommand.GetStatsResponse, error) {
	f.wait(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.stats[req.GetName()]
	if !ok {
		return nil, status.Errorf(codes.Unknown, "%s not found", req.GetName())
	}

	return &statscommand.GetStatsResponse{Stat: &statscommand.Stat{Name: req.GetName(), Value: v}}, nil
}

func startFakeAPI(t *testing.T) (*fakeAPI, *V2Ray) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{users: map[string]bool{}, stats: map[string]int64{}}
	srv := grpc.NewServer()
	proxymancommand.RegisterHandlerServiceServer(srv, api)
	statscommand.RegisterStatsServiceServer(srv, api)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	s := NewV2Ray()
	s.config = v2raytypes.NewConfig().WithDefaultValues()
	s.config.API.Port = uint16(lis.Addr().(*net.TCPAddr).Port)
	t.Cleanup(func() {
		if s.conn != nil {
			_ = s.conn.Close()
		}
	})

	return api, s
}

func TestPeersOverTheAPI(t *testing.T) {
	api, s := startFakeAPI(t)

	data := append([]byte{0x01}, []byte("0123456789abcdef")...)
	key := base64.StdEncoding.EncodeToString(data)
	if _, err := s.AddPeer(data); err != nil {
		t.Fatal(err)
	}
	if !api.users[key] || !s.HasPeer(data) || s.PeerCount() != 1 {
		t.Fatalf("peer not added: users %v", api.users)
	}

	// Counters the proxy has not created yet read as zero.
	peers, err := s.Peers()
	if err != nil || len(peers) != 1 || peers[0].Key != key || peers[0].Upload != 0 {
		t.Fatalf("peers %+v, %v", peers, err)
	}
	api.mu.Lock()
	api.stats["user>>>"+key+">>>traffic>>>uplink"] = 10
	api.stats["user>>>"+key+">>>traffic>>>downlink"] = 20
	api.mu.Unlock()
	if peers, err = s.Peers(); err != nil || peers[0].Upload != 10 || peers[0].Download != 20 {
		t.Fatalf("peers %+v, %v", peers, err)
	}

	if err := s.RemovePeer(data); err != nil {
		t.Fatal(err)
	}
	// A user the proxy no longer has is not an error.
	if err := s.RemovePeer(data); err != nil {
		t.Fatal(err)
	}
	if api.users[key] || s.HasPeer(data) {
		t.Fatal("peer not removed")
	}
}

// TestAPICallsAreBounded: a proxy that stops answering fails the call after
// rpcTimeout instead of holding the handshake or the job forever.
func TestAPICallsAreBounded(t *testing.T) {
	saved := rpcTimeout
	rpcTimeout = 200 * time.Millisecond
	t.Cleanup(func() { rpcTimeout = saved })

	api, s := startFakeAPI(t)
	api.stall = true

	start := time.Now()
	_, err := s.AddPeer(append([]byte{0x01}, []byte("0123456789abcdef")...))
	if err == nil || !strings.Contains(err.Error(), "DeadlineExceeded") {
		t.Fatalf("AddPeer against a stalled proxy: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("AddPeer took %s", elapsed)
	}
}
