package sider

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
)

type PluginConfig struct {
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config"`
}

type TLSConfig struct {
	// CertFile/KeyFile are used by QUIC listeners.
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`

	// ServerName is used by QUIC upstream dialing for TLS verification.
	ServerName string `json:"server_name"`

	// InsecureSkipVerify skips TLS verification when dialing QUIC upstreams.
	InsecureSkipVerify bool `json:"insecure_skip_verify"`

	// RootCAFile is an optional PEM bundle to trust when dialing QUIC upstreams.
	RootCAFile string `json:"root_ca_file"`

	// ALPN is the QUIC TLS application protocol (NextProtos). If empty, a default is used.
	ALPN string `json:"alpn"`
}

type ListenerConfig struct {
	Listen string `json:"listen"`
	// ListenNetwork supports "tcp" (default) and "quic".
	ListenNetwork string     `json:"listen_network"`
	ListenTLS     *TLSConfig `json:"listen_tls,omitempty"`

	Upstreams []string `json:"upstreams"`
	// UpstreamNetwork supports "tcp" (default) and "quic".
	UpstreamNetwork string     `json:"upstream_network"`
	UpstreamTLS     *TLSConfig `json:"upstream_tls,omitempty"`

	Plugins []PluginConfig `json:"plugins"`
}

type Config struct {
	// Listeners supports running multiple port-forward rules in one process.
	Listeners []ListenerConfig `json:"listeners"`

	// Backward-compatible single-listener fields.
	Listen    string         `json:"listen"`
	Upstreams []string       `json:"upstreams"`
	Plugins   []PluginConfig `json:"plugins"`

	DialTimeoutMs int `json:"dial_timeout_ms"`
}

func ParseConfig(b []byte) (Config, error) {
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, err
	}
	if len(cfg.Listeners) == 0 && (cfg.Listen != "" || len(cfg.Upstreams) > 0 || len(cfg.Plugins) > 0) {
		cfg.Listeners = append(cfg.Listeners, ListenerConfig{
			Listen:    cfg.Listen,
			Upstreams: cfg.Upstreams,
			Plugins:   cfg.Plugins,
		})
	}
	if len(cfg.Listeners) == 0 {
		return Config{}, errors.New("config: missing listeners")
	}
	for i, l := range cfg.Listeners {
		if l.Listen == "" {
			return Config{}, fmt.Errorf("config: listeners[%d].listen is required", i)
		}
		if len(l.Upstreams) == 0 {
			return Config{}, fmt.Errorf("config: listeners[%d].upstreams is required", i)
		}
		for j, u := range l.Upstreams {
			if u == "" {
				return Config{}, fmt.Errorf("config: listeners[%d].upstreams[%d] is empty", i, j)
			}
		}
		ln := normalizeNetwork(l.ListenNetwork)
		un := normalizeNetwork(l.UpstreamNetwork)
		if ln == "" {
			ln = "tcp"
			cfg.Listeners[i].ListenNetwork = "tcp"
		}
		if un == "" {
			un = "tcp"
			cfg.Listeners[i].UpstreamNetwork = "tcp"
		}
		if ln != "tcp" && ln != "quic" {
			return Config{}, fmt.Errorf("config: listeners[%d].listen_network must be tcp|quic", i)
		}
		if un != "tcp" && un != "quic" {
			return Config{}, fmt.Errorf("config: listeners[%d].upstream_network must be tcp|quic", i)
		}
		if ln == "quic" {
			if l.ListenTLS == nil || l.ListenTLS.CertFile == "" || l.ListenTLS.KeyFile == "" {
				return Config{}, fmt.Errorf("config: listeners[%d].listen_tls.cert_file/key_file is required for quic", i)
			}
		}
		if un == "quic" {
			tlsCfg := l.UpstreamTLS
			if tlsCfg == nil {
				return Config{}, fmt.Errorf("config: listeners[%d].upstream_tls is required for quic", i)
			}
			if tlsCfg.ServerName == "" && !tlsCfg.InsecureSkipVerify {
				host := upstreamHost(l.Upstreams[0])
				if host == "" || net.ParseIP(host) != nil {
					return Config{}, fmt.Errorf("config: listeners[%d].upstream_tls.server_name is required for quic when upstream host is ip", i)
				}
				tlsCfg.ServerName = host
				cfg.Listeners[i].UpstreamTLS = tlsCfg
			}
		}
	}
	if cfg.DialTimeoutMs <= 0 {
		cfg.DialTimeoutMs = 3000
	}
	return cfg, nil
}

func LoadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return ParseConfig(b)
}

func normalizeNetwork(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func upstreamHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return host
	}
	// Best-effort: accept plain host without port.
	return strings.TrimSpace(addr)
}
