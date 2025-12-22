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

// BBR 状态机
type bbrState int

const (
	bbrStartup bbrState = iota
	bbrDrain
	bbrProbeBW
	bbrProbeRTT
)

// BBR 配置
type Config struct {
	// 初始拥塞窗口大小（字节）
	InitialCwnd int64 `json:"initial_cwnd"`
	// 最小 RTT（毫秒）
	MinRTTMs int `json:"min_rtt_ms"`
	// ProbeRTT 阶段持续时间（毫秒）
	ProbeRTTDurationMs int `json:"probe_rtt_duration_ms"`
	// Startup 阶段的增益系数
	HighGain float64 `json:"high_gain"`
	// Drain 阶段的增益系数
	DrainGain float64 `json:"drain_gain"`
	// ProbeBW 阶段的增益周期
	ProbeBWGains []float64 `json:"probe_bw_gains"`
	// 带宽采样窗口大小
	BandwidthWindowSize int `json:"bandwidth_window_size"`
	// RTT 采样窗口大小
	RTTWindowSize int `json:"rtt_window_size"`
}

type Plugin struct {
	cfg Config
}

func init() {
	sider.RegisterPlugin("bbr", func() sider.Plugin { return &Plugin{} })
}

func (p *Plugin) Name() string { return "bbr" }

func (p *Plugin) Init(raw json.RawMessage) error {
	// 默认配置基于 Google BBR 论文
	p.cfg = Config{
		InitialCwnd:        10 * 1460, // 10 MSS
		MinRTTMs:           10,
		ProbeRTTDurationMs: 200,
		HighGain:           2.77,      // ln(2) * 4
		DrainGain:          1.0 / 2.77, // 1 / HighGain
		ProbeBWGains:       []float64{1.25, 0.75, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0}, // 8 阶段周期
		BandwidthWindowSize: 10,
		RTTWindowSize:      10,
	}

	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p.cfg); err != nil {
			return err
		}
	}

	if p.cfg.InitialCwnd <= 0 {
		return errors.New("bbr: initial_cwnd 必须 > 0")
	}
	if p.cfg.MinRTTMs <= 0 {
		return errors.New("bbr: min_rtt_ms 必须 > 0")
	}
	if p.cfg.HighGain <= 1.0 {
		return errors.New("bbr: high_gain 必须 > 1.0")
	}
	if p.cfg.DrainGain <= 0 {
		return errors.New("bbr: drain_gain 必须 > 0")
	}
	if len(p.cfg.ProbeBWGains) == 0 {
		return errors.New("bbr: probe_bw_gains 不能为空")
	}

	return nil
}

func (p *Plugin) Wrap(ctx context.Context, side sider.ConnSide, c net.Conn) net.Conn {
	return newBBRConn(c, p.cfg)
}

// BBR 连接包装器
type bbrConn struct {
	net.Conn
	bbr *bbrController
}

func newBBRConn(conn net.Conn, cfg Config) *bbrConn {
	return &bbrConn{
		Conn: conn,
		bbr:  newBBRController(cfg),
	}
}

func (c *bbrConn) Write(data []byte) (int, error) {
	return c.bbr.write(c.Conn, data)
}

func (c *bbrConn) Read(data []byte) (int, error) {
	n, err := c.Conn.Read(data)
	if n > 0 {
		c.bbr.onDataReceived(n)
	}
	return n, err
}

// BBR 控制器实现
type bbrController struct {
	cfg Config

	// 状态机
	mu           sync.Mutex
	state        bbrState
	stateStarted time.Time

	// 带宽估计
	maxBW       int64 // 字节/秒
	bwSamples   []bwSample
	bwFilterLen int

	// RTT 估计
	minRTT       time.Duration
	rttSamples   []time.Duration
	rttFilterLen int

	// 拥塞窗口
	cwnd     int64
	inflight int64

	// 步速控制
	pacingRate   int64
	nextSendTime time.Time

	// ProbeBW 周期
	probeBWCycleIdx   int
	probeBWCycleStart time.Time

	// ProbeRTT
	probeRTTStart time.Time

	// 传输追踪
	deliveredBytes int64
	deliveredTime  time.Time
}

type bwSample struct {
	bw   int64
	time time.Time
}

func newBBRController(cfg Config) *bbrController {
	now := time.Now()
	return &bbrController{
		cfg:           cfg,
		state:         bbrStartup,
		stateStarted:  now,
		maxBW:         cfg.InitialCwnd,
		bwFilterLen:   cfg.BandwidthWindowSize,
		minRTT:        time.Duration(cfg.MinRTTMs) * time.Millisecond,
		rttFilterLen:  cfg.RTTWindowSize,
		cwnd:          cfg.InitialCwnd,
		pacingRate:    cfg.InitialCwnd,
		nextSendTime:  now,
		deliveredTime: now,
	}
}

func (bbr *bbrController) write(conn net.Conn, data []byte) (int, error) {
	bbr.mu.Lock()
	defer bbr.mu.Unlock()

	totalWritten := 0

	for len(data) > 0 {
		// 检查是否可以发送更多数据（拥塞窗口限制）
		if bbr.inflight >= bbr.cwnd {
			// 等待一些数据被确认
			bbr.mu.Unlock()
			time.Sleep(time.Millisecond)
			bbr.mu.Lock()
			continue
		}

		// 计算可以发送的数据量
		canSend := bbr.cwnd - bbr.inflight
		if canSend > int64(len(data)) {
			canSend = int64(len(data))
		}

		// 步速控制：如果需要等待
		now := time.Now()
		if now.Before(bbr.nextSendTime) {
			delay := bbr.nextSendTime.Sub(now)
			bbr.mu.Unlock()
			time.Sleep(delay)
			bbr.mu.Lock()
			now = time.Now()
		}

		// 发送数据
		sendStart := now
		n, err := conn.Write(data[:canSend])
		sendEnd := time.Now()

		if n > 0 {
			bbr.onDataSent(n, sendStart, sendEnd)
			totalWritten += n
			data = data[n:]
		}

		if err != nil {
			return totalWritten, err
		}
	}

	return totalWritten, nil
}

func (bbr *bbrController) onDataSent(bytes int, sendStart, sendEnd time.Time) {
	bbr.inflight += int64(bytes)

	// 更新下一次发送时间
	if bbr.pacingRate > 0 {
		sendDuration := time.Duration(float64(time.Second) * float64(bytes) / float64(bbr.pacingRate))
		bbr.nextSendTime = sendEnd.Add(sendDuration)
	}

	// 更新传输追踪
	bbr.deliveredBytes += int64(bytes)
	bbr.deliveredTime = sendEnd
}

func (bbr *bbrController) onDataReceived(bytes int) {
	bbr.mu.Lock()
	defer bbr.mu.Unlock()

	// 这代表对已发送数据的确认
	bbr.inflight -= int64(bytes)
	if bbr.inflight < 0 {
		bbr.inflight = 0
	}

	// 估计 RTT
	now := time.Now()
	rtt := now.Sub(bbr.deliveredTime)
	if rtt > 0 {
		bbr.updateRTT(rtt)
	}

	// 估计带宽
	if !bbr.deliveredTime.IsZero() {
		duration := now.Sub(bbr.deliveredTime)
		if duration > 0 {
			bw := int64(float64(bytes) / duration.Seconds())
			bbr.updateBandwidth(bw, now)
		}
	}

	// 更新 BBR 状态机
	bbr.updateState(now)

	// 更新拥塞窗口和步速
	bbr.updateCwndAndPacing()
}

func (bbr *bbrController) updateBandwidth(bw int64, now time.Time) {
	// 添加新样本
	bbr.bwSamples = append(bbr.bwSamples, bwSample{bw: bw, time: now})

	// 只保留最近的样本
	if len(bbr.bwSamples) > bbr.bwFilterLen {
		bbr.bwSamples = bbr.bwSamples[1:]
	}

	// 更新最大带宽（滑动窗口最大值过滤）
	bbr.maxBW = 0
	for _, sample := range bbr.bwSamples {
		if sample.bw > bbr.maxBW {
			bbr.maxBW = sample.bw
		}
	}
}

func (bbr *bbrController) updateRTT(rtt time.Duration) {
	// 添加新样本
	bbr.rttSamples = append(bbr.rttSamples, rtt)

	// 只保留最近的样本
	if len(bbr.rttSamples) > bbr.rttFilterLen {
		bbr.rttSamples = bbr.rttSamples[1:]
	}

	// 更新最小 RTT
	for _, sample := range bbr.rttSamples {
		if sample < bbr.minRTT {
			bbr.minRTT = sample
		}
	}
}

func (bbr *bbrController) updateState(now time.Time) {
	switch bbr.state {
	case bbrStartup:
		// 当带宽停止增长时退出 Startup
		if len(bbr.bwSamples) >= 3 {
			recent := bbr.bwSamples[len(bbr.bwSamples)-1].bw
			older := bbr.bwSamples[len(bbr.bwSamples)-3].bw
			if recent < int64(float64(older)*1.25) { // 增长少于 25%
				bbr.state = bbrDrain
				bbr.stateStarted = now
			}
		}

	case bbrDrain:
		// 当 inflight 降至 BDP 时退出 Drain
		bdp := bbr.maxBW * int64(bbr.minRTT.Seconds())
		if bbr.inflight <= bdp {
			bbr.state = bbrProbeBW
			bbr.stateStarted = now
			bbr.probeBWCycleStart = now
		}

	case bbrProbeBW:
		// 每个 RTT 周期切换 ProbeBW 增益
		if now.Sub(bbr.probeBWCycleStart) > bbr.minRTT {
			bbr.probeBWCycleIdx = (bbr.probeBWCycleIdx + 1) % len(bbr.cfg.ProbeBWGains)
			bbr.probeBWCycleStart = now
		}

		// 如果最小 RTT 在 10 秒内未更新，进入 ProbeRTT
		if now.Sub(bbr.stateStarted) > 10*time.Second {
			bbr.state = bbrProbeRTT
			bbr.stateStarted = now
			bbr.probeRTTStart = now
		}

	case bbrProbeRTT:
		// 在 ProbeRTT 中停留指定的持续时间
		if now.Sub(bbr.probeRTTStart) > time.Duration(bbr.cfg.ProbeRTTDurationMs)*time.Millisecond {
			bbr.state = bbrProbeBW
			bbr.stateStarted = now
			bbr.probeBWCycleStart = now
			bbr.probeBWCycleIdx = 0
		}
	}
}

func (bbr *bbrController) updateCwndAndPacing() {
	var gain float64

	switch bbr.state {
	case bbrStartup:
		gain = bbr.cfg.HighGain

	case bbrDrain:
		gain = bbr.cfg.DrainGain

	case bbrProbeBW:
		gain = bbr.cfg.ProbeBWGains[bbr.probeBWCycleIdx]

	case bbrProbeRTT:
		gain = 1.0
		// 在 ProbeRTT 中将 cwnd 减少到 4 个数据包
		bbr.cwnd = 4 * 1460 // 4 MSS
		bbr.pacingRate = bbr.maxBW
		return
	}

	// 计算 BDP（带宽延迟乘积）
	// BDP = 带宽(字节/秒) * RTT(秒)
	rttSeconds := bbr.minRTT.Seconds()
	bdp := int64(float64(bbr.maxBW) * rttSeconds)
	if bdp <= 0 {
		bdp = bbr.cfg.InitialCwnd
	}

	// 更新拥塞窗口
	bbr.cwnd = int64(float64(bdp) * gain)
	if bbr.cwnd < bbr.cfg.InitialCwnd {
		bbr.cwnd = bbr.cfg.InitialCwnd
	}

	// 更新步速
	bbr.pacingRate = int64(float64(bbr.maxBW) * gain)
	if bbr.pacingRate < bbr.cfg.InitialCwnd {
		bbr.pacingRate = bbr.cfg.InitialCwnd
	}
}
