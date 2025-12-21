package gray

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"net"
	"time"

	"sider/internal/sider"
)

type Config struct {
	Stable        string `json:"stable"`
	Canary        string `json:"canary"`
	CanaryPercent int    `json:"canary_percent"`

	// Salt changes hashing to reshuffle users.
	Salt string `json:"salt"`
}

type Plugin struct {
	cfg Config
}

func init() {
	sider.RegisterPlugin("gray", func() sider.Plugin { return &Plugin{} })
}

func (p *Plugin) Name() string { return "gray" }

func (p *Plugin) Init(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("gray: config is required")
	}
	if err := json.Unmarshal(raw, &p.cfg); err != nil {
		return err
	}
	if p.cfg.Stable == "" || p.cfg.Canary == "" {
		return errors.New("gray: stable/canary is required")
	}
	if p.cfg.CanaryPercent < 0 || p.cfg.CanaryPercent > 100 {
		return errors.New("gray: canary_percent must be 0..100")
	}
	return nil
}

// SelectUpstream 选择流量上游
func (p *Plugin) SelectUpstream(ctx context.Context, info sider.ConnInfo, candidates []string) (string, error) {
	_ = ctx
	_ = candidates
	ip := remoteIP(info.RemoteAddr)
	if ip == "" {
		ip = time.Now().Format("20060102150405")
	}
	if pickCanary(ip, p.cfg.Salt, p.cfg.CanaryPercent) {
		return p.cfg.Canary, nil
	}
	return p.cfg.Stable, nil
}

// pickCanary 基于客户端 IP 的灰度流量分流
func pickCanary(key, salt string, percent int) bool {
	if percent <= 0 {
		return false
	}
	if percent >= 100 {
		return true
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(salt))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(key))
	v := h.Sum32() % 100
	return int(v) < percent
}

func remoteIP(a net.Addr) string {
	ta, ok := a.(*net.TCPAddr)
	if !ok || ta == nil || ta.IP == nil {
		return ""
	}
	return ta.IP.String()
}
