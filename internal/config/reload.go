package config

import (
	"context"
	"log/slog"
	"os"
	"time"
)

// Watcher polls a workflow file's mtime on a fixed cadence and pushes the
// reloaded+validated workflow to the supplied callback. Polling is preferred
// over fsnotify here because it adds no dependency, is cross-platform, and a
// 2s detection latency is well within the operational tolerance for
// workflow edits (which are rare relative to ticket flow).
type Watcher struct {
	path     string
	interval time.Duration
	log      *slog.Logger
}

type WatcherOptions struct {
	Path     string
	Interval time.Duration // optional, defaults to 2s
	Logger   *slog.Logger
}

func NewWatcher(opts WatcherOptions) *Watcher {
	interval := opts.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Watcher{path: opts.Path, interval: interval, log: logger}
}

// Run blocks until ctx is canceled. Each tick stats the file; if mtime
// advanced, it loads + validates the workflow and invokes onReload. Reload
// errors are logged and the previous valid config is implicitly retained
// (the callback is not invoked).
func (w *Watcher) Run(ctx context.Context, onReload func(*Workflow)) {
	last, _ := mtime(w.path)
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cur, err := mtime(w.path)
			if err != nil {
				w.log.Error("workflow stat", "path", w.path, "err", err)
				continue
			}
			if !cur.After(last) {
				continue
			}
			last = cur
			wf, err := Load(w.path)
			if err != nil {
				w.log.Error("workflow reload load failed; keeping previous", "err", err)
				continue
			}
			if err := wf.Validate(); err != nil {
				w.log.Error("workflow reload invalid; keeping previous", "err", err)
				continue
			}
			w.log.Info("workflow reloaded", "path", w.path)
			onReload(wf)
		}
	}
}

func mtime(path string) (time.Time, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime(), nil
}
