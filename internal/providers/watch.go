package providers

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// SubscribeChanges watches the directory, not individual files, so atomic
// editor replacements and new definitions wake even an entirely manual system.
// Like the database subscription, unexpected closure means the consumer must
// stop rather than silently continue with stale scheduling configuration.
func (r *Registry) SubscribeChanges(ctx context.Context) (<-chan struct{}, func(), error) {
	if err := r.acquire(); err != nil {
		return nil, nil, err
	}
	defer r.release()
	path, err := filepath.Abs(r.root.Name())
	if err != nil {
		return nil, nil, errors.New("provider directory cannot be watched")
	}
	pinned, err := r.root.Stat(".")
	if err != nil {
		return nil, nil, errors.New("provider directory cannot be watched")
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, nil, errors.New("provider directory change subscription is unavailable")
	}
	if err := watcher.Add(path); err != nil {
		_ = watcher.Close()
		return nil, nil, errors.New("provider directory cannot be watched")
	}
	current, err := os.Stat(path)
	if err != nil || !os.SameFile(pinned, current) {
		_ = watcher.Close()
		return nil, nil, errors.New("provider directory changed while opening its watcher")
	}
	watchCtx, cancel := context.WithCancel(ctx)
	changes := make(chan struct{}, 1)
	go func() {
		defer close(changes)
		defer watcher.Close()
		signal := func() {
			select {
			case changes <- struct{}{}:
			default:
			}
		}
		for {
			select {
			case <-watchCtx.Done():
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Name == path && event.Has(fsnotify.Remove|fsnotify.Rename) {
					return
				}
				if definitionFilename(filepath.Base(event.Name)) {
					signal()
				}
			case err, ok := <-watcher.Errors:
				if !ok || !errors.Is(err, fsnotify.ErrEventOverflow) {
					return
				}
				// Overflow discards event detail, not the directory watch. A full
				// authoritative snapshot repairs any coalesced or lost changes.
				signal()
			}
		}
	}()
	return changes, cancel, nil
}
