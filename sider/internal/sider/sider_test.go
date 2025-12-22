package sider

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
)

// 简单的上游服务器
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
					// 回显数据
					_, _ = conn.Write(buf[:n])
				}
			}(c)
		}
	}()

	return ln, nil
}

func TestProxyBasic(t *testing.T) {
	// 启动上游服务器
	upstreamLn, err := startUpstreamServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("start upstream failed: %v", err)
	}
	defer upstreamLn.Close()

	upstreamAddr := upstreamLn.Addr().String()

	// 创建代理配置
	cfg := Config{
		DialTimeoutMs: 5000,
		Listeners: []ListenerConfig{
			{
				Listen:    "127.0.0.1:0",
				Upstreams: []string{upstreamAddr},
			},
		},
	}

	// 创建代理
	proxy, err := NewProxy(cfg.Listeners[0], time.Duration(cfg.DialTimeoutMs)*time.Millisecond)
	if err != nil {
		t.Fatalf("create proxy failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动代理
	go func() {
		_ = proxy.Serve(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// 连接到代理
	conn, err := net.Dial("tcp", proxy.ListenAddr())
	if err != nil {
		t.Fatalf("dial proxy failed: %v", err)
	}
	defer conn.Close()

	// 发送数据
	testData := []byte("hello world")
	_, err = conn.Write(testData)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 读取回显
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	if string(buf[:n]) != string(testData) {
		t.Errorf("expected %s, got %s", testData, buf[:n])
	}
}

func BenchmarkProxyThroughput(b *testing.B) {
	// 启动上游服务器
	upstreamLn, err := startUpstreamServer("127.0.0.1:0")
	if err != nil {
		b.Fatalf("start upstream failed: %v", err)
	}
	defer upstreamLn.Close()

	upstreamAddr := upstreamLn.Addr().String()

	// 创建代理
	cfg := Config{
		DialTimeoutMs: 5000,
		Listeners: []ListenerConfig{
			{
				Listen:    "127.0.0.1:0",
				Upstreams: []string{upstreamAddr},
			},
		},
	}

	proxy, err := NewProxy(cfg.Listeners[0], time.Duration(cfg.DialTimeoutMs)*time.Millisecond)
	if err != nil {
		b.Fatalf("create proxy failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = proxy.Serve(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		conn, err := net.Dial("tcp", proxy.ListenAddr())
		if err != nil {
			b.Fatalf("dial proxy failed: %v", err)
		}

		data := make([]byte, 4096)
		for j := 0; j < 10; j++ {
			_, _ = conn.Write(data)
			_, _ = conn.Read(data)
		}
		conn.Close()
	}
}

func TestSiderWithPprof(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pprof test in short mode")
	}

	// 创建 pprof 输出目录
	pprofDir := "pprof_output"
	os.MkdirAll(pprofDir, 0755)

	// 启动 CPU profiling
	cpuFile, err := os.Create(fmt.Sprintf("%s/sider_cpu.prof", pprofDir))
	if err != nil {
		t.Fatalf("create cpu profile failed: %v", err)
	}
	defer cpuFile.Close()

	if err := pprof.StartCPUProfile(cpuFile); err != nil {
		t.Fatalf("start cpu profile failed: %v", err)
	}
	defer pprof.StopCPUProfile()

	// 启动内存 profiling
	memFile, err := os.Create(fmt.Sprintf("%s/sider_mem.prof", pprofDir))
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

	// 创建代理
	cfg := Config{
		DialTimeoutMs: 5000,
		Listeners: []ListenerConfig{
			{
				Listen:    "127.0.0.1:0",
				Upstreams: []string{upstreamAddr},
			},
		},
	}

	proxy, err := NewProxy(cfg.Listeners[0], time.Duration(cfg.DialTimeoutMs)*time.Millisecond)
	if err != nil {
		t.Fatalf("create proxy failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = proxy.Serve(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// 运行负载测试
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
				conn, err := net.Dial("tcp", proxy.ListenAddr())
				if err != nil {
					atomic.AddInt64(&errorCount, 1)
					continue
				}

				data := make([]byte, 1024)
				for j := 0; j < 5; j++ {
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

	cancel()

	t.Logf("CPU profile written to %s/sider_cpu.prof", pprofDir)
	t.Logf("Memory profile written to %s/sider_mem.prof", pprofDir)
	t.Logf("Success: %d, Errors: %d", atomic.LoadInt64(&successCount), atomic.LoadInt64(&errorCount))
	t.Logf("Generate flamegraph with: go tool pprof -http=:8080 %s/sider_cpu.prof", pprofDir)
}

func TestConfigParsing(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{
			name: "valid config",
			config: `{
				"dial_timeout_ms": 5000,
				"listeners": [
					{
						"listen": "127.0.0.1:8080",
						"upstreams": ["127.0.0.1:9000"]
					}
				]
			}`,
			wantErr: false,
		},
		{
			name: "missing upstreams",
			config: `{
				"dial_timeout_ms": 5000,
				"listeners": [
					{
						"listen": "127.0.0.1:8080"
					}
				]
			}`,
			wantErr: true,
		},
		{
			name: "missing listen",
			config: `{
				"dial_timeout_ms": 5000,
				"listeners": [
					{
						"upstreams": ["127.0.0.1:9000"]
					}
				]
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			err := json.Unmarshal([]byte(tt.config), &cfg)
			if err != nil {
				if !tt.wantErr {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}

			// 验证配置
			if len(cfg.Listeners) == 0 {
				if !tt.wantErr {
					t.Errorf("expected listeners")
				}
				return
			}

			_, err = NewProxy(cfg.Listeners[0], time.Duration(cfg.DialTimeoutMs)*time.Millisecond)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewProxy error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
