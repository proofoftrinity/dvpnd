// SPDX-License-Identifier: Apache-2.0

package types

import (
	"bytes"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/viper"

	"github.com/trinitystake/dvpnd/v9/utils"
)

var (
	ct = strings.TrimSpace(`
# Name of the tunnel interface
interface = {{ toml .Interface }}

# Port number to accept the incoming connections
listen_port = {{ .ListenPort }}

# Transport: "udp" (recommended) or "tcp"
proto = {{ toml .Proto }}

# Network interface that carries the node's internet traffic; peers are NAT-ed
# through it. Empty means detect it from the default route at start
uplink = {{ toml .Uplink }}

# Hand each peer an IPv6 tunnel address next to the IPv4 one; the host must
# then reach the IPv6 internet. Set it false for an IPv4-only tunnel
enable_ipv6 = {{ .EnableIPv6 }}

[management]
# Loopback port of OpenVPN's management interface, over which the node admits
# clients and reads their traffic; nothing else may bind it
port = {{ .Management.Port }}
	`)

	t = utils.ConfigTemplate("openvpn_toml", ct)
)

type ManagementConfig struct {
	Port uint16 `json:"port" mapstructure:"port"`
}

type Config struct {
	Interface  string            `json:"interface" mapstructure:"interface"`
	ListenPort uint16            `json:"listen_port" mapstructure:"listen_port"`
	Proto      string            `json:"proto" mapstructure:"proto"`
	Uplink     string            `json:"uplink" mapstructure:"uplink"`
	EnableIPv6 bool              `json:"enable_ipv6" mapstructure:"enable_ipv6"`
	Management *ManagementConfig `json:"management" mapstructure:"management"`
}

func NewConfig() *Config {
	return &Config{Management: &ManagementConfig{}}
}

func (c *Config) Validate() error {
	if c.Interface == "" || strings.ContainsAny(c.Interface, " /\n") || len(c.Interface) > 15 {
		return errors.New("interface must be a valid interface name")
	}
	if c.ListenPort == 0 {
		return errors.New("listen_port cannot be zero")
	}
	if c.Proto != ProtoUDP && c.Proto != ProtoTCP {
		return errors.Errorf("proto must be %q or %q", ProtoUDP, ProtoTCP)
	}
	if strings.ContainsAny(c.Uplink, " \n") {
		return errors.New("invalid uplink")
	}
	if c.Management.Port == 0 {
		return errors.New("management port cannot be zero")
	}
	if c.Management.Port == c.ListenPort {
		return errors.New("management port must differ from listen_port")
	}

	return nil
}

func (c *Config) WithDefaultValues() *Config {
	c.Interface = "ovpn0"
	c.ListenPort = utils.RandomPort()
	c.Proto = ProtoUDP
	c.EnableIPv6 = true
	c.Management.Port = utils.RandomPort()
	for c.Management.Port == c.ListenPort {
		c.Management.Port = utils.RandomPort()
	}

	return c
}

func (c *Config) SaveToPath(path string) error {
	var buf bytes.Buffer
	if err := t.Execute(&buf, c); err != nil {
		return err
	}

	return utils.WritePrivateFile(path, buf.Bytes())
}

func (c *Config) String() string {
	var buf bytes.Buffer
	if err := t.Execute(&buf, c); err != nil {
		panic(err)
	}

	return buf.String()
}

func ReadInConfig(v *viper.Viper) (*Config, error) {
	config := NewConfig().WithDefaultValues()
	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}
	if err := v.Unmarshal(config); err != nil {
		return nil, err
	}

	return config, nil
}
