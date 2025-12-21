package sidercfg

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
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`

	ServerName         string `json:"server_name"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify"`
	RootCAFile         string `json:"root_ca_file"`
	ALPN               string `json:"alpn"`
}

type ListenerConfig struct {
	Listen string `json:"listen"`

	ListenNetwork string     `json:"listen_network"`
	ListenTLS     *TLSConfig `json:"listen_tls,omitempty"`

	Upstreams []string `json:"upstreams"`

	UpstreamNetwork string     `json:"upstream_network"`
	UpstreamTLS     *TLSConfig `json:"upstream_tls,omitempty"`

	Plugins []PluginConfig `json:"plugins"`
}

// Config is the runtime config format expected by `sider/`.
//
// Keep this file independent from `sider/` (router and sider are separate modules).
type Config struct {
	Listeners []ListenerConfig `json:"listeners"`

	// Backward-compatible single-listener fields.
	Listen    string         `json:"listen"`
	Upstreams []string       `json:"upstreams"`
	Plugins   []PluginConfig `json:"plugins"`

	DialTimeoutMs int `json:"dial_timeout_ms"`
}

func Parse(b []byte) (Config, error) {
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

func MarshalCanonical(cfg Config) ([]byte, error) {
	// json.Marshal produces compact single-line JSON which is SSE-friendly.
	return json.Marshal(cfg)
}

type Loader interface {
	Load() ([]byte, error)
}

type FileLoader struct {
	Path string
}

func (l FileLoader) Load() ([]byte, error) {
	return os.ReadFile(l.Path)
}

func (l FileLoader) Stat() (os.FileInfo, error) {
	return os.Stat(l.Path)
}

func normalizeNetwork(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func upstreamHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(addr)
}
