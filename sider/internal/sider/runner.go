package sider

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

type Runner struct {
	parentCtx context.Context
	fatalCh   chan<- error

	mu     sync.Mutex
	active *run
}

func NewRunner(parentCtx context.Context, fatalCh chan<- error) *Runner {
	return &Runner{
		parentCtx: parentCtx,
		fatalCh:   fatalCh,
	}
}

func (r *Runner) Apply(cfg Config) error {
	log.Printf("runner apply: listeners=%d", len(cfg.Listeners))
	proxies, err := buildProxies(cfg)
	if err != nil {
		return err
	}

	r.stopActive()

	ctx, cancel := context.WithCancel(r.parentCtx)
	done := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(len(proxies))
	for _, p := range proxies {
		go func(p *Proxy) {
			defer wg.Done()
			if err := p.Serve(ctx); err != nil && ctx.Err() == nil {
				select {
				case r.fatalCh <- fmt.Errorf("serve %s: %w", p.ListenAddr(), err):
				default:
				}
			}
		}(p)
	}
	go func() {
		wg.Wait()
		close(done)
	}()

	r.mu.Lock()
	r.active = &run{cancel: cancel, done: done}
	r.mu.Unlock()

	return nil
}

func (r *Runner) Stop() {
	r.stopActive()
}

type run struct {
	cancel context.CancelFunc
	done   <-chan struct{}
}

func (r *Runner) stopActive() {
	r.mu.Lock()
	active := r.active
	r.active = nil
	r.mu.Unlock()

	if active == nil {
		return
	}
	log.Printf("runner stopping active listeners")
	active.cancel()
	<-active.done
	log.Printf("runner active listeners stopped")
}

func buildProxies(cfg Config) ([]*Proxy, error) {
	dialTimeout := time.Duration(cfg.DialTimeoutMs) * time.Millisecond
	proxies := make([]*Proxy, 0, len(cfg.Listeners))
	for _, l := range cfg.Listeners {
		p, err := NewProxy(l, dialTimeout)
		if err != nil {
			return nil, err
		}
		proxies = append(proxies, p)
	}
	return proxies, nil
}
