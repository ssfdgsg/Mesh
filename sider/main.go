package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sider/internal/sider"
	"sider/internal/sider/controlplane"

	_ "net/http/pprof"

	_ "sider/internal/sider/plugins/bbr"
	_ "sider/internal/sider/plugins/gray"
)

func main() {
	var cfgPath string
	var pprofAddr string
	var controlPlane string
	var node string
	flag.StringVar(&cfgPath, "config", "./sider/config.example.json", "config file path (json)")
	flag.StringVar(&pprofAddr, "pprof", "", "pprof listen address, e.g. :6060 (empty disables)")
	flag.StringVar(&controlPlane, "control-plane", "", "control plane base url (enables config streaming), e.g. http://127.0.0.1:8081")
	flag.StringVar(&node, "node", "", "optional node id for control plane")
	flag.Parse()

	cfg, err := sider.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	log.Printf("config loaded: path=%s listeners=%d dial_timeout_ms=%d", cfgPath, len(cfg.Listeners), cfg.DialTimeoutMs)

	//信号监听
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if pprofAddr != "" {
		srv := &http.Server{Addr: pprofAddr, Handler: http.DefaultServeMux}
		go func() {
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("pprof server error: %v", err)
				cancel()
			}
		}()
		go func() {
			<-ctx.Done()
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer shutdownCancel()
			_ = srv.Shutdown(shutdownCtx)
		}()
		log.Printf("pprof enabled: http://127.0.0.1%s/debug/pprof/", pprofAddr)
	}

	//初始化代理
	errCh := make(chan error, 1)
	runner := sider.NewRunner(ctx, errCh)
	if err := runner.Apply(cfg); err != nil {
		log.Fatalf("apply config: %v", err)
	}
	log.Printf("sider start: listeners=%d", len(cfg.Listeners))

	if controlPlane != "" {
		c := controlplane.Client{BaseURL: controlPlane, Node: node}
		updates, streamErrs := c.StreamConfigs(ctx)
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case b, ok := <-updates:
					if !ok {
						return
					}
					nextCfg, err := sider.ParseConfig(b)
					if err != nil {
						log.Printf("control plane config parse: %v", err)
						continue
					}
					if err := runner.Apply(nextCfg); err != nil {
						log.Printf("apply streamed config: %v", err)
						continue
					}
					log.Printf("config applied from control plane: listeners=%d", len(nextCfg.Listeners))
				case err, ok := <-streamErrs:
					if ok && err != nil {
						log.Printf("control plane stream: %v", err)
					}
				}
			}
		}()
		log.Printf("control plane enabled: %s", controlPlane)
	}

	//启动代理

	select {
	case <-ctx.Done():
	case err := <-errCh:
		log.Printf("error: %v", err)
		cancel()
	}
	runner.Stop()
	log.Printf("sider exit")
}
