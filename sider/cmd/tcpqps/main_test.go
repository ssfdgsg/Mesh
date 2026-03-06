package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"
	"testing"
	"time"
)

// 简单的测试服务器
func startTestServer(addr string) (net.Listener, error) {
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
				buf := make([]byte, 1024)
				for {
					n, err := conn.Read(buf)
					if err != nil {
						return
					}
					// 回显数据
					conn.Write(buf[:n])
				}
			}(c)
		}
	}()

	return ln, nil
}

func TestTcpqpsBasic(t *testing.T) {
	ln, err := startTestServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("start server failed: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()

	// 测试单个连接
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	// 发送数据
	data := []byte("test")
	_, err = conn.Write(data)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 读取回显
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	if n != len(data) {
		t.Errorf("expected %d bytes, got %d", len(data), n)
	}
}

func BenchmarkTcpqpsQPS(b *testing.B) {
	ln, err := startTestServer("127.0.0.1:0")
	if err != nil {
		b.Fatalf("start server failed: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		d := net.Dialer{Timeout: 2 * time.Second}
		conn, err := d.Dial("tcp", addr)
		if err != nil {
			b.Fatalf("dial failed: %v", err)
		}
		conn.Close()
	}
}

func TestTcpqpsWithPprof(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pprof test in short mode")
	}

	// 创建 pprof 输出目录
	pprofDir := "pprof_output"
	os.MkdirAll(pprofDir, 0755)

	// 启动 CPU profiling
	cpuFile, err := os.Create(fmt.Sprintf("%s/tcpqps_cpu.prof", pprofDir))
	if err != nil {
		t.Fatalf("create cpu profile failed: %v", err)
	}
	defer cpuFile.Close()

	if err := pprof.StartCPUProfile(cpuFile); err != nil {
		t.Fatalf("start cpu profile failed: %v", err)
	}
	defer pprof.StopCPUProfile()

	// 启动内存 profiling
	memFile, err := os.Create(fmt.Sprintf("%s/tcpqps_mem.prof", pprofDir))
	if err != nil {
		t.Fatalf("create mem profile failed: %v", err)
	}
	defer memFile.Close()

	// 启动测试服务器
	ln, err := startTestServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("start server failed: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()

	// 运行 QPS 测试
	concurrency := 50
	duration := 5 * time.Second
	timeout := 2 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			d := net.Dialer{Timeout: timeout}
			for ctx.Err() == nil {
				conn, err := d.DialContext(ctx, "tcp", addr)
				if err != nil {
					continue
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

	t.Logf("CPU profile written to %s/tcpqps_cpu.prof", pprofDir)
	t.Logf("Memory profile written to %s/tcpqps_mem.prof", pprofDir)
	t.Logf("Generate flamegraph with: go tool pprof -http=:8080 %s/tcpqps_cpu.prof", pprofDir)
}
