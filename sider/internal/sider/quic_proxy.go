package sider

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"

	"github.com/quic-go/quic-go"
)

const defaultQUICALPN = "sider-quic"

func (p *Proxy) serveQUIC(ctx context.Context) error {
	tlsConf, err := buildQUICServerTLSConfig(p.listenTLS)
	if err != nil {
		return err
	}

	ln, err := quic.ListenAddr(p.listenAddr, tlsConf, &quic.Config{})
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
		conn, err := ln.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go p.handleQUICConn(ctx, conn)
	}
}

func (p *Proxy) handleQUICConn(ctx context.Context, conn *quic.Conn) {
	for {
		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			return
		}
		go p.handleConn(ctx, newQUICStreamConn(conn, stream, false))
	}
}

func buildQUICServerTLSConfig(cfg *TLSConfig) (*tls.Config, error) {
	if cfg == nil || cfg.CertFile == "" || cfg.KeyFile == "" {
		return nil, fmt.Errorf("quic: listen_tls.cert_file/key_file is required")
	}
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("quic: load cert/key: %w", err)
	}
	alpn := cfg.ALPN
	if alpn == "" {
		alpn = defaultQUICALPN
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{alpn},
	}, nil
}
