package bbr

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPluginInit(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{
			name:    "默认配置",
			config:  `{}`,
			wantErr: false,
		},
		{
			name: "自定义配置",
			config: `{
				"initial_cwnd": 20000,
				"min_rtt_ms": 20,
				"high_gain": 3.0,
				"drain_gain": 0.5,
				"probe_bw_gains": [1.5, 0.5, 1.0]
			}`,
			wantErr: false,
		},
		{
			name:    "无效的 initial_cwnd",
			config:  `{"initial_cwnd": -1}`,
			wantErr: true,
		},
		{
			name:    "无效的 min_rtt_ms",
			config:  `{"min_rtt_ms": 0}`,
			wantErr: true,
		},
		{
			name:    "无效的 high_gain",
			config:  `{"high_gain": 0.5}`,
			wantErr: true,
		},
		{
			name:    "无效的 drain_gain",
			config:  `{"drain_gain": 0}`,
			wantErr: true,
		},
		{
			name:    "空的 probe_bw_gains",
			config:  `{"probe_bw_gains": []}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Plugin{}
			err := p.Init(json.RawMessage(tt.config))
			if (err != nil) != tt.wantErr {
				t.Errorf("Init() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPluginName(t *testing.T) {
	p := &Plugin{}
	if name := p.Name(); name != "bbr" {
		t.Errorf("Name() = %v, want bbr", name)
	}
}

func TestBBRController_StateTransitions(t *testing.T) {
	cfg := Config{
		InitialCwnd:        14600,
		MinRTTMs:           10,
		ProbeRTTDurationMs: 200,
		HighGain:           2.77,
		DrainGain:          0.36,
		ProbeBWGains:       []float64{1.25, 0.75, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0},
		BandwidthWindowSize: 10,
		RTTWindowSize:      10,
	}

	bbr := newBBRController(cfg)

	// 初始状态应该是 Startup
	if bbr.state != bbrStartup {
		t.Errorf("初始状态 = %v, 期望 %v", bbr.state, bbrStartup)
	}

	// 模拟带宽样本不增长
	now := time.Now()
	bbr.bwSamples = []bwSample{
		{bw: 1000000, time: now.Add(-3 * time.Second)},
		{bw: 1100000, time: now.Add(-2 * time.Second)},
		{bw: 1050000, time: now.Add(-1 * time.Second)},
	}

	bbr.updateState(now)

	// 应该转换到 Drain
	if bbr.state != bbrDrain {
		t.Errorf("无带宽增长后，状态 = %v, 期望 %v", bbr.state, bbrDrain)
	}
}

func TestBBRController_BandwidthEstimation(t *testing.T) {
	cfg := Config{
		InitialCwnd:         14600,
		MinRTTMs:            10,
		HighGain:            2.77,
		DrainGain:           0.36,
		ProbeBWGains:        []float64{1.25},
		BandwidthWindowSize: 10,
		RTTWindowSize:       10,
	}

	bbr := newBBRController(cfg)
	now := time.Now()

	// 添加带宽样本
	samples := []int64{1000000, 1500000, 1200000, 1800000, 1600000}
	for i, bw := range samples {
		bbr.updateBandwidth(bw, now.Add(time.Duration(i)*time.Second))
	}

	// 最大带宽应该是最高的样本
	if bbr.maxBW != 1800000 {
		t.Errorf("maxBW = %d, 期望 1800000", bbr.maxBW)
	}
}

func TestBBRController_RTTEstimation(t *testing.T) {
	cfg := Config{
		InitialCwnd:         14600,
		MinRTTMs:            100,
		HighGain:            2.77,
		DrainGain:           0.36,
		ProbeBWGains:        []float64{1.25},
		BandwidthWindowSize: 10,
		RTTWindowSize:       10,
	}

	bbr := newBBRController(cfg)

	// 添加 RTT 样本
	samples := []time.Duration{
		50 * time.Millisecond,
		60 * time.Millisecond,
		40 * time.Millisecond,
		55 * time.Millisecond,
	}

	for _, rtt := range samples {
		bbr.updateRTT(rtt)
	}

	// 最小 RTT 应该是最小的样本
	if bbr.minRTT != 40*time.Millisecond {
		t.Errorf("minRTT = %v, 期望 40ms", bbr.minRTT)
	}
}

func TestBBRController_CwndCalculation(t *testing.T) {
	cfg := Config{
		InitialCwnd:         14600,
		MinRTTMs:            10,
		HighGain:            2.77,
		DrainGain:           0.36,
		ProbeBWGains:        []float64{1.25},
		BandwidthWindowSize: 10,
		RTTWindowSize:       10,
	}

	bbr := newBBRController(cfg)
	bbr.maxBW = 1000000 // 1 MB/s
	bbr.minRTT = 100 * time.Millisecond

	// 测试 Startup 状态
	bbr.state = bbrStartup
	bbr.updateCwndAndPacing()

	// BDP = 1000000 * 0.1 = 100000
	expectedBDP := int64(100000)
	expectedCwnd := int64(float64(expectedBDP) * cfg.HighGain)

	if bbr.cwnd != expectedCwnd {
		t.Errorf("Startup cwnd = %d, 期望 %d", bbr.cwnd, expectedCwnd)
	}

	// 测试 Drain 状态
	bbr.state = bbrDrain
	bbr.updateCwndAndPacing()

	expectedCwnd = int64(float64(expectedBDP) * cfg.DrainGain)
	if bbr.cwnd != expectedCwnd {
		t.Errorf("Drain cwnd = %d, 期望 %d", bbr.cwnd, expectedCwnd)
	}

	// 测试 ProbeRTT 状态
	bbr.state = bbrProbeRTT
	bbr.updateCwndAndPacing()

	expectedCwnd = 4 * 1460 // 4 MSS
	if bbr.cwnd != expectedCwnd {
		t.Errorf("ProbeRTT cwnd = %d, 期望 %d", bbr.cwnd, expectedCwnd)
	}
}
