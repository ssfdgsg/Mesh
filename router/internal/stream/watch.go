package stream

import (
	"context"
	"log"
	"os"
	"time"

	"router/internal/sidercfg"
)

// PollFile is a minimal watcher for loader-backed config (typically a local file).
// 轮询查看配置文件更新
// It reloads periodically and triggers onChange when file metadata changes.
func PollFile(ctx context.Context, loader sidercfg.Loader, interval time.Duration, onChange func()) {
	type statter interface {
		Stat() (os.FileInfo, error)
	}

	var lastMod time.Time
	var lastSize int64
	var lastStatErr string

	t := time.NewTicker(interval)
	defer t.Stop()

	check := func() bool {
		s, ok := loader.(statter)
		if !ok {
			// No stat support, always trigger.
			return true
		}
		fi, err := s.Stat()
		if err != nil {
			msg := err.Error()
			if msg != lastStatErr {
				log.Printf("config stat error: %v", err)
				lastStatErr = msg
			}
			return false
		}
		if lastStatErr != "" {
			log.Printf("config stat recovered")
			lastStatErr = ""
		}
		mod := fi.ModTime()
		size := fi.Size()
		changed := mod.After(lastMod) || size != lastSize
		lastMod = mod
		lastSize = size
		return changed
	}

	// First tick is handled by initial broadcast in NewMux.
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if check() {
				onChange()
			}
		}
	}
}
