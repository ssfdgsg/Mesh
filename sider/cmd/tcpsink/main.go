package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	var listenAddr string
	var closeImmediately bool
	var idleTimeout time.Duration

	flag.StringVar(&listenAddr, "listen", "127.0.0.1:9000", "listen address")
	flag.BoolVar(&closeImmediately, "close-immediately", false, "close connections immediately after accept")
	flag.DurationVar(&idleTimeout, "idle-timeout", 5*time.Second, "read deadline while draining (0 disables)")
	flag.Parse()

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	log.Printf("tcpsink listening on %s", listenAddr)
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("accept: %v", err)
			continue
		}
		go handle(c, closeImmediately, idleTimeout)
	}
}

func handle(c net.Conn, closeImmediately bool, idleTimeout time.Duration) {
	defer c.Close()

	if closeImmediately {
		return
	}

	buf := make([]byte, 32*1024)
	for {
		if idleTimeout > 0 {
			_ = c.SetReadDeadline(time.Now().Add(idleTimeout))
		}
		_, err := c.Read(buf)
		if err != nil {
			return
		}
	}
}
