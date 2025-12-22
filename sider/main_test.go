package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sider/internal/sider"
)

// 启动上游服务器
func startUpstreamServer(addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				buf := make([]byte, 4096)
				for {
					n, err := conn.Read(buf)
					if err != nil {
						return
					}
					_, _ = conn.Write(buf[:n])
				}
			}(c)
		}
	}()

	return ln, nil
}

func TestMainSiderWithBBRAndPprof(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pprof test in short mode")
	}

	// 创建 pprof 输出目录
	pprofDir := "pprof_output"
	os.MkdirAll(pprofDir, 0755)

	// 启动 CPU profiling
	cpuFile, err := os.Create(fmt.Sprintf("%s/main_sider_cpu.prof", pprofDir))
	if err != nil {
		t.Fatalf("create cpu profile failed: %v", err)
	}
	defer cpuFile.Close()

	if err := pprof.StartCPUProfile(cpuFile); err != nil {
		t.Fatalf("start cpu profile failed: %v", err)
	}
	defer pprof.StopCPUProfile()

	// 启动内存 profiling
	memFile, err := os.Create(fmt.Sprintf("%s/main_sider_mem.prof", pprofDir))
	if err != nil {
		t.Fatalf("create mem profile failed: %v", err)
	}
	defer memFile.Close()

	// 启动上游服务器
	upstreamLn, err := startUpstreamServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("start upstream failed: %v", err)
	}
	defer upstreamLn.Close()

	upstreamAddr := upstreamLn.Addr().String()

	// 使用固定端口
	fixedProxyPort := "127.0.0.1:19999"

	// 创建配置，包含 BBR 插件
	cfg := sider.Config{
		DialTimeoutMs: 5000,
		Listeners: []sider.ListenerConfig{
			{
				Listen:    fixedProxyPort,
				Upstreams: []string{upstreamAddr},
				Plugins: []sider.PluginConfig{
					{
						Name: "bbr",
						Config: json.RawMessage(`{
							"initial_cwnd": 14600,
							"min_rtt_ms": 10,
							"probe_rtt_duration_ms": 200,
							"high_gain": 2.77,
							"drain_gain": 0.36,
							"probe_bw_gains": [1.25, 0.75, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0],
							"bandwidth_window_size": 10,
							"rtt_window_size": 10
						}`),
					},
				},
			},
		},
	}

	// 创建 runner
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	runner := sider.NewRunner(ctx, errCh)

	// 应用配置
	if err := runner.Apply(cfg); err != nil {
		t.Fatalf("apply config failed: %v", err)
	}
	defer runner.Stop()

	time.Sleep(200 * time.Millisecond)

	// 运行负载测试 - 通过代理连接
	concurrency := 50
	duration := 5 * time.Second
	testCtx, testCancel := context.WithTimeout(context.Background(), duration)
	defer testCancel()

	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64

	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			for testCtx.Err() == nil {
				// 连接到代理
				conn, err := net.Dial("tcp", fixedProxyPort)
				if err != nil {
					atomic.AddInt64(&errorCount, 1)
					continue
				}

				data := make([]byte, 4096)
				for j := 0; j < 20; j++ {
					_, err = conn.Write(data)
					if err != nil {
						break
					}
					_, err = conn.Read(data)
					if err != nil {
						break
					}
				}

				if err == nil {
					atomic.AddInt64(&successCount, 1)
				} else {
					atomic.AddInt64(&errorCount, 1)
				}
				conn.Close()
			}
		}()
	}

	wg.Wait()

	// 写入内存 profile
	runtime.GC()
	if err := pprof.WriteHeapProfile(memFile); err != nil {
		t.Fatalf("write heap profile failed: %v", err)
	}

	// 获取当前工作目录
	pwd, _ := os.Getwd()

	// 输出结果和链接
	t.Logf("")
	t.Logf("=== Sider BBR Performance Profile ===")
	t.Logf("")
	t.Logf("✓ CPU profile: %s/main_sider_cpu.prof", pprofDir)
	t.Logf("✓ Memory profile: %s/main_sider_mem.prof", pprofDir)
	t.Logf("✓ SVG flamegraph: file://%s/%s/main_sider_cpu.svg", pwd, pprofDir)
	t.Logf("")
	t.Logf("Performance Metrics:")
	t.Logf("  Success connections: %d", atomic.LoadInt64(&successCount))
	t.Logf("  Failed connections: %d", atomic.LoadInt64(&errorCount))
	t.Logf("  Success rate: %.2f%%", float64(atomic.LoadInt64(&successCount))*100/float64(atomic.LoadInt64(&successCount)+atomic.LoadInt64(&errorCount)))
	t.Logf("")
	t.Logf("View interactive profile:")
	t.Logf("  go tool pprof -http=:8080 %s/main_sider_cpu.prof", pprofDir)
	t.Logf("")
}
