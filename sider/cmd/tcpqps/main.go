package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	var addr string
	var concurrency int
	var duration time.Duration
	var timeout time.Duration
	var writeBytes int
	var readBytes int

	flag.StringVar(&addr, "addr", "127.0.0.1:8080", "target address")
	flag.IntVar(&concurrency, "c", 200, "concurrent workers")
	flag.DurationVar(&duration, "d", 30*time.Second, "test duration")
	flag.DurationVar(&timeout, "timeout", 2*time.Second, "per-connection dial/read/write timeout")
	flag.IntVar(&writeBytes, "write-bytes", 0, "bytes to write after connect (0 disables)")
	flag.IntVar(&readBytes, "read-bytes", 0, "bytes to read before close (0 disables)")
	flag.Parse()

	if concurrency <= 0 {
		fatalf("invalid -c: %d", concurrency)
	}
	if duration <= 0 {
		fatalf("invalid -d: %s", duration)
	}
	if timeout <= 0 {
		fatalf("invalid -timeout: %s", timeout)
	}
	if writeBytes < 0 || readBytes < 0 {
		fatalf("invalid -write-bytes/-read-bytes")
	}

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var ok uint64
	var errs uint64
	var samples uint64
	h := newHist()

	writeBuf := make([]byte, writeBytes)
	for i := range writeBuf {
		writeBuf[i] = 'a'
	}
	readBuf := make([]byte, readBytes)

	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			d := net.Dialer{Timeout: timeout}
			for ctx.Err() == nil {
				opStart := time.Now()

				conn, err := d.DialContext(ctx, "tcp", addr)
				if err != nil {
					atomic.AddUint64(&errs, 1)
					h.observe(time.Since(opStart))
					continue
				}

				if writeBytes > 0 {
					_ = conn.SetWriteDeadline(time.Now().Add(timeout))
					_, err = conn.Write(writeBuf)
					if err != nil {
						_ = conn.Close()
						atomic.AddUint64(&errs, 1)
						h.observe(time.Since(opStart))
						continue
					}
				}

				if readBytes > 0 {
					_ = conn.SetReadDeadline(time.Now().Add(timeout))
					_, err = io.ReadFull(conn, readBuf)
					if err != nil {
						_ = conn.Close()
						atomic.AddUint64(&errs, 1)
						h.observe(time.Since(opStart))
						continue
					}
				}

				_ = conn.Close()
				atomic.AddUint64(&ok, 1)
				atomic.AddUint64(&samples, 1)
				h.observe(time.Since(opStart))
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	okN := atomic.LoadUint64(&ok)
	errN := atomic.LoadUint64(&errs)
	sampleN := atomic.LoadUint64(&samples)
	qps := float64(okN) / elapsed.Seconds()

	p50 := h.percentile(0.50)
	p90 := h.percentile(0.90)
	p99 := h.percentile(0.99)

	fmt.Printf("tcpqps: addr=%s c=%d d=%s timeout=%s write=%dB read=%dB\n", addr, concurrency, duration, timeout, writeBytes, readBytes)
	fmt.Printf("runtime: go=%s gomaxprocs=%d\n", runtime.Version(), runtime.GOMAXPROCS(0))
	fmt.Printf("result: elapsed=%s ok=%d errs=%d qps=%.0f samples=%d\n", elapsed.Truncate(time.Millisecond), okN, errN, qps, sampleN)
	fmt.Printf("latency: p50=%s p90=%s p99=%s\n", p50, p90, p99)
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(2)
}

type hist struct {
	bounds []time.Duration
	counts []uint64
	total  uint64
}

func newHist() *hist {
	b := []time.Duration{
		50 * time.Microsecond,
		100 * time.Microsecond,
		200 * time.Microsecond,
		500 * time.Microsecond,
		1 * time.Millisecond,
		2 * time.Millisecond,
		5 * time.Millisecond,
		10 * time.Millisecond,
		20 * time.Millisecond,
		50 * time.Millisecond,
		100 * time.Millisecond,
		200 * time.Millisecond,
		500 * time.Millisecond,
		1 * time.Second,
		2 * time.Second,
		5 * time.Second,
	}
	return &hist{
		bounds: b,
		counts: make([]uint64, len(b)+1),
	}
}

func (h *hist) observe(d time.Duration) {
	i := h.bucket(d)
	atomic.AddUint64(&h.counts[i], 1)
	atomic.AddUint64(&h.total, 1)
}

func (h *hist) bucket(d time.Duration) int {
	for i, b := range h.bounds {
		if d <= b {
			return i
		}
	}
	return len(h.bounds)
}

func (h *hist) percentile(p float64) time.Duration {
	if p <= 0 {
		return 0
	}
	total := atomic.LoadUint64(&h.total)
	if total == 0 {
		return 0
	}
	if p >= 1 {
		return h.bounds[len(h.bounds)-1]
	}
	want := uint64(float64(total) * p)
	if want == 0 {
		want = 1
	}

	var cum uint64
	for i := 0; i < len(h.counts); i++ {
		cum += atomic.LoadUint64(&h.counts[i])
		if cum >= want {
			if i >= len(h.bounds) {
				return h.bounds[len(h.bounds)-1]
			}
			return h.bounds[i]
		}
	}
	return h.bounds[len(h.bounds)-1]
}

