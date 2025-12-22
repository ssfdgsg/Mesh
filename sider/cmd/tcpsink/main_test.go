package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"runtime/pprof"
	"testing"
	"time"
)

func TestTcpsinkBasic(t *testing.T) {
	// 启动 tcpsink 服务器
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()

	// 在 goroutine 中处理连接
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handle(c, false, 5*time.Second)
		}
	}()

	// 连接并发送数据
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	// 发送数据
	data := []byte("hello world")
	_, err = conn.Write(data)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// 等待一下让服务器处理
	time.Sleep(100 * time.Millisecond)
}

func TestTcpsinkCloseImmediately(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handle(c, true, 0)
		}
	}()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	// 连接应该立即关闭
	buf := make([]byte, 1024)
	_, err = conn.Read(buf)
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func BenchmarkTcpsinkThroughput(b *testing.B) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handle(c, false, 5*time.Second)
		}
	}()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			b.Fatalf("dial failed: %v", err)
		}

		data := make([]byte, 4096)
		for j := 0; j < 100; j++ {
			_, err = conn.Write(data)
			if err != nil {
				b.Fatalf("write failed: %v", err)
			}
		}
		conn.Close()
	}
}

func TestTcpsinkWithPprof(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pprof test in short mode")
	}

	// 创建 pprof 输出目录
	pprofDir := "pprof_output"
	os.MkdirAll(pprofDir, 0755)

	// 启动 CPU profiling
	cpuFile, err := os.Create(fmt.Sprintf("%s/tcpsink_cpu.prof", pprofDir))
	if err != nil {
		t.Fatalf("create cpu profile failed: %v", err)
	}
	defer cpuFile.Close()

	if err := pprof.StartCPUProfile(cpuFile); err != nil {
		t.Fatalf("start cpu profile failed: %v", err)
	}
	defer pprof.StopCPUProfile()

	// 启动内存 profiling
	memFile, err := os.Create(fmt.Sprintf("%s/tcpsink_mem.prof", pprofDir))
	if err != nil {
		t.Fatalf("create mem profile failed: %v", err)
	}
	defer memFile.Close()

	// 启动 tcpsink 服务器
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handle(c, false, 5*time.Second)
		}
	}()

	// 运行负载测试
	for i := 0; i < 100; i++ {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatalf("dial failed: %v", err)
		}

		data := make([]byte, 8192)
		for j := 0; j < 50; j++ {
			_, err = conn.Write(data)
			if err != nil {
				break
			}
		}
		conn.Close()
	}

	// 写入内存 profile
	runtime.GC()
	if err := pprof.WriteHeapProfile(memFile); err != nil {
		t.Fatalf("write heap profile failed: %v", err)
	}

	t.Logf("CPU profile written to %s/tcpsink_cpu.prof", pprofDir)
	t.Logf("Memory profile written to %s/tcpsink_mem.prof", pprofDir)
	t.Logf("Generate flamegraph with: go tool pprof -http=:8080 %s/tcpsink_cpu.prof", pprofDir)
}
