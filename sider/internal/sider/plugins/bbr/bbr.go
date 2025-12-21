package bbr

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"

	"sider/internal/sider"
)

type Config struct {
	// MinBps/MaxBps clamp pacing rate.
	MinBps int64 `json:"min_bps"`
	MaxBps int64 `json:"max_bps"`

	// Gain scales estimated bandwidth (e.g. 1.25).
	Gain float64 `json:"gain"`

	// UpdateMs controls how often bandwidth estimate updates.
	UpdateMs int `json:"update_ms"`
}

type Plugin struct {
	cfg   Config
	pacer *pacer
}

func init() {
	sider.RegisterPlugin("bbr", func() sider.Plugin { return &Plugin{} })
}

func (p *Plugin) Name() string { return "bbr" }

func (p *Plugin) Init(raw json.RawMessage) error {
	p.cfg = Config{
		MinBps:   64 * 1024,
		MaxBps:   64 * 1024 * 1024,
		Gain:     1.25,
		UpdateMs: 500,
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p.cfg); err != nil {
			return err
		}
	}
	if p.cfg.MinBps <= 0 || p.cfg.MaxBps <= 0 || p.cfg.MaxBps < p.cfg.MinBps {
		return errors.New("bbr: invalid min_bps/max_bps")
	}
	if p.cfg.Gain <= 0 {
		return errors.New("bbr: gain must be > 0")
	}
	if p.cfg.UpdateMs <= 0 {
		return errors.New("bbr: update_ms must be > 0")
	}
	p.pacer = newPacer(p.cfg.MinBps, p.cfg.MaxBps, p.cfg.Gain, time.Duration(p.cfg.UpdateMs)*time.Millisecond)
	return nil
}

func (p *Plugin) Wrap(ctx context.Context, side sider.ConnSide, c net.Conn) net.Conn {
	_ = ctx
	_ = side
	return &pacedConn{Conn: c, pacer: p.pacer}
}

type pacedConn struct {
	net.Conn
	pacer *pacer
}

func (c *pacedConn) Write(p []byte) (int, error) {
	const chunk = 32 * 1024
	written := 0
	for len(p) > 0 {
		nn := len(p)
		if nn > chunk {
			nn = chunk
		}
		if err := c.pacer.waitN(nn); err != nil {
			return written, err
		}
		n, err := c.Conn.Write(p[:nn])
		if n > 0 {
			c.pacer.observe(n)
			written += n
			p = p[n:]
			continue
		}
		return written, err
	}
	return written, nil
}

type pacer struct {
	minBps int64
	maxBps int64
	gain   float64
	tick   time.Duration

	mu        sync.Mutex
	nextWrite time.Time
	rateBps   int64

	obsMu     sync.Mutex
	obsBytes  int64
	bwWindow  []int64
	bwWinSize int
}

func newPacer(minBps, maxBps int64, gain float64, tick time.Duration) *pacer {
	p := &pacer{
		minBps:    minBps,
		maxBps:    maxBps,
		gain:      gain,
		tick:      tick,
		rateBps:   minBps,
		bwWinSize: 10,
		bwWindow:  make([]int64, 0, 10),
		nextWrite: time.Now(),
	}
	go p.loop()
	return p
}

func (p *pacer) loop() {
	t := time.NewTicker(p.tick)
	defer t.Stop()
	for range t.C {
		bytes := p.swapObs()
		if bytes <= 0 {
			continue
		}
		bps := int64(float64(bytes) / p.tick.Seconds())
		p.pushBW(bps)
		maxBW := p.maxBW()
		target := int64(float64(maxBW) * p.gain)
		if target < p.minBps {
			target = p.minBps
		}
		if target > p.maxBps {
			target = p.maxBps
		}
		p.mu.Lock()
		p.rateBps = target
		p.mu.Unlock()
	}
}

func (p *pacer) swapObs() int64 {
	p.obsMu.Lock()
	defer p.obsMu.Unlock()
	b := p.obsBytes
	p.obsBytes = 0
	return b
}

func (p *pacer) observe(n int) {
	p.obsMu.Lock()
	p.obsBytes += int64(n)
	p.obsMu.Unlock()
}

func (p *pacer) pushBW(bps int64) {
	p.obsMu.Lock()
	defer p.obsMu.Unlock()
	p.bwWindow = append(p.bwWindow, bps)
	if len(p.bwWindow) > p.bwWinSize {
		p.bwWindow = p.bwWindow[len(p.bwWindow)-p.bwWinSize:]
	}
}

func (p *pacer) maxBW() int64 {
	p.obsMu.Lock()
	defer p.obsMu.Unlock()
	var m int64
	for _, v := range p.bwWindow {
		if v > m {
			m = v
		}
	}
	if m <= 0 {
		return p.minBps
	}
	return m
}

func (p *pacer) waitN(n int) error {
	p.mu.Lock()
	rate := p.rateBps
	now := time.Now()
	if p.nextWrite.Before(now) {
		p.nextWrite = now
	}
	delay := p.nextWrite.Sub(now)
	advance := time.Duration(float64(time.Second) * (float64(n) / float64(rate)))
	p.nextWrite = p.nextWrite.Add(advance)
	p.mu.Unlock()

	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	<-timer.C
	return nil
}

