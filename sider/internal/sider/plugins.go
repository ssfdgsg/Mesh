package sider

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

type ConnInfo struct {
	AcceptedAt time.Time
	LocalAddr  net.Addr
	RemoteAddr net.Addr
}

type Plugin interface {
	Name() string
	Init(cfg json.RawMessage) error
}

type ConnGate interface {
	AllowConn(ctx context.Context, info ConnInfo) (allow bool, reason string)
}

type Router interface {
	// SelectUpstream 根据连接信息做路由决策
	SelectUpstream(ctx context.Context, info ConnInfo, candidates []string) (addr string, err error)
}

type ConnWrapper interface {
	Wrap(ctx context.Context, side ConnSide, c net.Conn) net.Conn
}

type ConnSide int

const (
	ClientSide ConnSide = iota
	UpstreamSide
)

type pluginFactory func() Plugin

var (
	registryMu sync.RWMutex
	registry   = map[string]pluginFactory{}
)

func RegisterPlugin(name string, f pluginFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if name == "" {
		panic("sider: plugin name is empty")
	}
	if f == nil {
		panic("sider: plugin factory is nil")
	}
	if _, exists := registry[name]; exists {
		panic("sider: plugin already registered: " + name)
	}
	registry[name] = f
}

func newPlugin(pc PluginConfig) (Plugin, error) {
	registryMu.RLock()
	f := registry[pc.Name]
	registryMu.RUnlock()
	if f == nil {
		return nil, fmt.Errorf("unknown plugin: %q", pc.Name)
	}
	p := f()
	if p == nil {
		return nil, fmt.Errorf("plugin factory returned nil: %q", pc.Name)
	}
	if p.Name() == "" {
		return nil, fmt.Errorf("plugin %q has empty Name()", pc.Name)
	}
	if err := p.Init(pc.Config); err != nil {
		return nil, fmt.Errorf("init plugin %q: %w", pc.Name, err)
	}
	return p, nil
}

type pluginSet struct {
	all      []Plugin
	gates    []ConnGate
	routers  []Router
	wrappers []ConnWrapper
}

func loadPlugins(pcs []PluginConfig) (pluginSet, error) {
	var ps pluginSet
	for _, pc := range pcs {
		p, err := newPlugin(pc)
		if err != nil {
			return pluginSet{}, err
		}
		ps.all = append(ps.all, p)
		hasGate := false
		hasRouter := false
		hasWrapper := false
		if g, ok := p.(ConnGate); ok {
			ps.gates = append(ps.gates, g)
			hasGate = true
		}
		if r, ok := p.(Router); ok {
			ps.routers = append(ps.routers, r)
			hasRouter = true
		}
		if w, ok := p.(ConnWrapper); ok {
			ps.wrappers = append(ps.wrappers, w)
			hasWrapper = true
		}
		log.Printf("plugin loaded: name=%s gate=%t router=%t wrapper=%t", p.Name(), hasGate, hasRouter, hasWrapper)
	}
	return ps, nil
}
