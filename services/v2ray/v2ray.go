// SPDX-License-Identifier: Apache-2.0
// Modified from sentinel-official/dvpn-node @ 62bde16 (2024-01-25). See NOTICE.

package v2ray

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/viper"
	proxymancommand "github.com/v2fly/v2ray-core/v5/app/proxyman/command"
	statscommand "github.com/v2fly/v2ray-core/v5/app/stats/command"
	"github.com/v2fly/v2ray-core/v5/common/protocol"
	"github.com/v2fly/v2ray-core/v5/common/serial"
	"github.com/v2fly/v2ray-core/v5/common/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/trinitystake/dvpnd/v9/services/common"
	v2raytypes "github.com/trinitystake/dvpnd/v9/services/v2ray/types"
	"github.com/trinitystake/dvpnd/v9/types"
	"github.com/trinitystake/dvpnd/v9/utils"
)

const (
	InfoLen = 2 + 1 + 1
)

var (
	_ types.Service = (*V2Ray)(nil)
)

type V2Ray struct {
	info    []byte
	process *common.Process
	config  *v2raytypes.Config
	peers   *v2raytypes.Peers
	tlsPin  string
}

// binaryName is the proxy binary looked up on PATH; a variable so tests can
// point it at a stub.
var binaryName = "v2ray"

// stopTimeout is how long Stop waits for the child to exit after SIGTERM
// before killing it.
var stopTimeout = 5 * time.Second

func NewV2Ray() *V2Ray {
	return &V2Ray{
		info:   make([]byte, InfoLen),
		config: v2raytypes.NewConfig(),
		peers:  v2raytypes.NewPeers(),
	}
}

func (s *V2Ray) configFilePath() string {
	return filepath.Join(os.TempDir(), "v2ray_config.json")
}

func (s *V2Ray) Type() uint64 {
	return v2raytypes.Type
}

func (s *V2Ray) Info() []byte {
	return s.info
}

// TLSPin is the hex SHA-256 of the certificate presented on the TLS inbound,
// which clients pin because the node's certificate is self-signed. Empty when
// TLS is disabled.
func (s *V2Ray) TLSPin() string {
	return s.tlsPin
}

func (s *V2Ray) Init(home string) (err error) {
	if _, err = exec.LookPath(binaryName); err != nil {
		return fmt.Errorf("the %q binary is not on PATH: install it on the host "+
			"(docs/operator.md, section 6) or run the Docker image, which bundles it: %w",
			binaryName, err)
	}

	v := viper.New()
	v.SetConfigFile(filepath.Join(home, v2raytypes.ConfigFileName))

	s.config, err = v2raytypes.ReadInConfig(v)
	if err != nil {
		return err
	}
	if err = s.config.Validate(); err != nil {
		return err
	}

	if s.config.VMess.TLS {
		s.config.VMess.Security = "tls"
	}
	s.config.VMess.TLSCertPath = filepath.Join(home, "tls.crt")
	s.config.VMess.TLSKeyPath = filepath.Join(home, "tls.key")
	if s.config.VMess.TLS {
		s.tlsPin, err = common.CertificatePin(s.config.VMess.TLSCertPath)
		if err != nil {
			return err
		}
	}

	t, err := template.New("v2ray_json").Funcs(template.FuncMap{"json": common.JSON}).Parse(configTemplate)
	if err != nil {
		return err
	}

	data := templateData{
		Config:          s.config,
		BlockedNetworks: common.BlockedNetworks(),
		BlockedDomain:   common.BlockedLocalDomain,
		SMTPPort:        common.SMTPPort,
		AllowSMTP:       common.EgressPolicy().AllowSMTP,
	}

	var buf bytes.Buffer
	if err = t.Execute(&buf, data); err != nil {
		return err
	}
	if err = os.WriteFile(s.configFilePath(), buf.Bytes(), 0600); err != nil {
		return err
	}

	binary.BigEndian.PutUint16(s.info[0:], s.config.VMess.ListenPort)
	transport := v2raytypes.NewTransportFromString(s.config.VMess.Transport)
	s.info[2] = transport.Byte()
	s.info[3] = utils.ByteFromBool(s.config.VMess.TLS)

	return nil
}

func (s *V2Ray) Start() (err error) {
	s.process, err = common.StartProcess(binaryName,
		[]string{"run", "--config", s.configFilePath()},
		[]string{"V2RAY_VMESS_AEAD_FORCED=false"})

	return err
}

// Stop asks the proxy to exit and waits for it, killing it after stopTimeout.
func (s *V2Ray) Stop() error {
	return s.process.Stop(stopTimeout)
}

func (s *V2Ray) clientConn() (*grpc.ClientConn, error) {
	target := fmt.Sprintf("127.0.0.1:%d", s.config.API.Port)
	return grpc.Dial(
		target,
		grpc.WithBlock(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}

func (s *V2Ray) handlerServiceClient() (*grpc.ClientConn, proxymancommand.HandlerServiceClient, error) {
	conn, err := s.clientConn()
	if err != nil {
		return nil, nil, err
	}

	client := proxymancommand.NewHandlerServiceClient(conn)
	return conn, client, nil
}

func (s *V2Ray) statsServiceClient() (*grpc.ClientConn, statscommand.StatsServiceClient, error) {
	conn, err := s.clientConn()
	if err != nil {
		return nil, nil, err
	}

	client := statscommand.NewStatsServiceClient(conn)
	return conn, client, nil
}

func (s *V2Ray) AddPeer(data []byte) (result []byte, err error) {
	if len(data) != 1+16 {
		return nil, errors.New("data length must be 17 bytes")
	}

	conn, client, err := s.handlerServiceClient()
	if err != nil {
		return nil, err
	}

	defer func() {
		if err = conn.Close(); err != nil {
			panic(err)
		}
	}()

	var (
		email  = base64.StdEncoding.EncodeToString(data)
		proxy  = v2raytypes.Proxy(data[0])
		uid, _ = uuid.ParseBytes(data[1:])
	)

	req := &proxymancommand.AlterInboundRequest{
		Tag: proxy.Tag(),
		Operation: serial.ToTypedMessage(
			&proxymancommand.AddUserOperation{
				User: &protocol.User{
					Level:   0,
					Email:   email,
					Account: proxy.Account(uid),
				},
			},
		),
	}

	_, err = client.AlterInbound(context.TODO(), req)
	if err != nil {
		return nil, err
	}

	s.peers.Put(
		v2raytypes.Peer{
			Email: email,
		},
	)

	return result, nil
}

func (s *V2Ray) HasPeer(data []byte) bool {
	var (
		email = base64.StdEncoding.EncodeToString(data)
		peer  = s.peers.Get(email)
	)

	return !peer.Empty()
}

func (s *V2Ray) RemovePeer(data []byte) error {
	if len(data) != 1+16 {
		return errors.New("data length must be 17 bytes")
	}

	conn, client, err := s.handlerServiceClient()
	if err != nil {
		return err
	}

	defer func() {
		if err = conn.Close(); err != nil {
			panic(err)
		}
	}()

	var (
		email = base64.StdEncoding.EncodeToString(data)
		proxy = v2raytypes.Proxy(data[0])
	)

	req := &proxymancommand.AlterInboundRequest{
		Tag: proxy.Tag(),
		Operation: serial.ToTypedMessage(
			&proxymancommand.RemoveUserOperation{
				Email: email,
			},
		),
	}

	_, err = client.AlterInbound(context.TODO(), req)
	if err != nil {
		if !strings.Contains(err.Error(), "not found") {
			return err
		}
	}

	s.peers.Delete(email)

	return nil
}

func (s *V2Ray) Peers() (items []types.Peer, err error) {
	conn, client, err := s.statsServiceClient()
	if err != nil {
		return nil, err
	}

	defer func() {
		if err = conn.Close(); err != nil {
			panic(err)
		}
	}()

	err = s.peers.Iterate(
		func(key string, _ v2raytypes.Peer) (bool, error) {
			req := &statscommand.GetStatsRequest{
				Reset_: false,
				Name:   fmt.Sprintf("user>>>%s>>>traffic>>>uplink", key),
			}

			res, err := client.GetStats(context.TODO(), req)
			if err != nil {
				if !strings.Contains(err.Error(), "not found") {
					return false, err
				}
			}

			upLink := res.GetStat()
			if upLink == nil {
				upLink = &statscommand.Stat{}
			}

			req = &statscommand.GetStatsRequest{
				Reset_: false,
				Name:   fmt.Sprintf("user>>>%s>>>traffic>>>downlink", key),
			}

			res, err = client.GetStats(context.TODO(), req)
			if err != nil {
				if !strings.Contains(err.Error(), "not found") {
					return false, err
				}
			}

			downLink := res.GetStat()
			if downLink == nil {
				downLink = &statscommand.Stat{}
			}

			items = append(
				items,
				types.Peer{
					Key:      key,
					Upload:   upLink.GetValue(),
					Download: downLink.GetValue(),
				},
			)
			return false, nil
		},
	)

	if err != nil {
		return nil, err
	}

	return items, nil
}

func (s *V2Ray) PeerCount() int {
	return s.peers.Len()
}
