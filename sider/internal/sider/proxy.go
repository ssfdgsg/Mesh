package sider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// 缓冲区池
var bufPool = sync.Pool{
	New: func() any { b := make([]byte, 32*1024); return &b },
}

type Proxy struct {
	listenAddr    string
	listenNetwork string
	listenTLS     *TLSConfig

	upstreams       []string
	upstreamNetwork string
	upstreamTLS     *TLSConfig

	dialTimeout time.Duration
	plugins     pluginSet
}

func NewProxy(l ListenerConfig, dialTimeout time.Duration) (*Proxy, error) {
	if l.Listen == "" {
		return nil, errors.New("listen is required")
	}
	if len(l.Upstreams) == 0 {
		return nil, errors.New("upstreams is required")
	}
	ps, err := loadPlugins(l.Plugins)
	if err != nil {
		return nil, err
	}
	return &Proxy{
		listenAddr:      l.Listen,
		listenNetwork:   normalizeNetwork(l.ListenNetwork),
		listenTLS:       l.ListenTLS,
		upstreams:       append([]string(nil), l.Upstreams...),
		upstreamNetwork: normalizeNetwork(l.UpstreamNetwork),
		upstreamTLS:     l.UpstreamTLS,
		dialTimeout:     dialTimeout,
		plugins:         ps,
	}, nil
}

func (p *Proxy) ListenAddr() string { return p.listenAddr }

func (p *Proxy) Serve(ctx context.Context) error {
	switch normalizeNetwork(p.listenNetwork) {
	case "", "tcp":
		return p.serveTCP(ctx)
	case "quic":
		return p.serveQUIC(ctx)
	default:
		return fmt.Errorf("unknown listen_network: %q", p.listenNetwork)
	}
}

func (p *Proxy) serveTCP(ctx context.Context) error {
	ln, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		return err
	}
	defer ln.Close()
	log.Printf("proxy listen: %s", p.String())
	defer log.Printf("proxy stop: %s", p.String())

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		c, err := ln.Accept()
		if err != nil {
			//错误处理
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Temporary() {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return err
		}
		go p.handleConn(ctx, c)
	}
}

func (p *Proxy) handleConn(ctx context.Context, client net.Conn) {
	defer client.Close()

	info := ConnInfo{
		AcceptedAt: time.Now(),
		LocalAddr:  client.LocalAddr(),
		RemoteAddr: client.RemoteAddr(),
	}

	for _, g := range p.plugins.gates {
		allow, reason := g.AllowConn(ctx, info)
		if !allow {
			if reason != "" {
				log.Printf("conn rejected: local=%s remote=%s reason=%s", info.LocalAddr, info.RemoteAddr, reason)
			}
			return
		}
	}

	upstreamAddr := p.upstreams[0]
	for _, r := range p.plugins.routers {
		//把客户端流量转发到的后端服务
		addr, err := r.SelectUpstream(ctx, info, p.upstreams)
		if err != nil {
			log.Printf("select upstream error: local=%s remote=%s err=%v", info.LocalAddr, info.RemoteAddr, err)
			return
		}
		if addr != "" {
			upstreamAddr = addr
		}
	}

	upstream, err := p.dialUpstream(ctx, upstreamAddr)
	if err != nil {
		log.Printf(
			"dial upstream failed: addr=%s network=%s local=%s remote=%s err=%v",
			upstreamAddr,
			normalizeNetwork(p.upstreamNetwork),
			info.LocalAddr,
			info.RemoteAddr,
			err,
		)
		return
	}
	defer upstream.Close()

	//封装连接
	for _, w := range p.plugins.wrappers {
		client = w.Wrap(ctx, ClientSide, client)
		upstream = w.Wrap(ctx, UpstreamSide, upstream)
	}

	pipe(ctx, client, upstream)
}

func pipe(ctx context.Context, a net.Conn, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		buf := bufPool.Get().(*[]byte)
		n, _ := io.CopyBuffer(b, a, *buf)
		if n > 0 {
			log.Printf("message received: from=%s to=%s bytes=%d", a.RemoteAddr(), b.RemoteAddr(), n)
		}
		bufPool.Put(buf)
		closeWrite(b)
	}()
	go func() {
		defer wg.Done()
		buf := bufPool.Get().(*[]byte)
		n, _ := io.CopyBuffer(a, b, *buf)
		if n > 0 {
			log.Printf("message received: from=%s to=%s bytes=%d", b.RemoteAddr(), a.RemoteAddr(), n)
		}
		bufPool.Put(buf)
		closeWrite(a)
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
	case <-done:
	}
}

func closeWrite(c net.Conn) {
	type closeWriter interface{ CloseWrite() error }
	if cw, ok := c.(closeWriter); ok {
		_ = cw.CloseWrite()
		return
	}
	_ = c.Close()
}

func (p *Proxy) String() string {
	return fmt.Sprintf(
		"listen=%s(%s) upstreams=%v(%s) plugins=%d",
		p.listenAddr,
		normalizeNetwork(p.listenNetwork),
		p.upstreams,
		normalizeNetwork(p.upstreamNetwork),
		len(p.plugins.all),
	)
}
