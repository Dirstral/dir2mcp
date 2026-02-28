package index

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Dirstral/dir2mcp/internal/model"
)

type IndexedFile struct {
	Name  string
	Path  string
	Index model.Index
}

type PersistenceManager struct {
	indices  []IndexedFile
	interval time.Duration
	onError  func(error)

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewPersistenceManager(indices []IndexedFile, interval time.Duration, onError func(error)) *PersistenceManager {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	return &PersistenceManager{
		indices:  indices,
		interval: interval,
		onError:  onError,
	}
}

func (m *PersistenceManager) LoadAll(ctx context.Context) error {
	for _, idx := range m.indices {
		if idx.Index == nil {
			continue
		}
		if err := idx.Index.Load(idx.Path); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	return nil
}

func (m *PersistenceManager) SaveAll() error {
	var combined error
	for _, idx := range m.indices {
		if idx.Index == nil {
			continue
		}
		if err := idx.Index.Save(idx.Path); err != nil {
			combined = errors.Join(combined, err)
		}
	}
	return combined
}

func (m *PersistenceManager) Start(ctx context.Context) {
	if len(m.indices) == 0 {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if err := m.SaveAll(); err != nil {
					m.emitError(err)
				}
			}
		}
	}()
}

func (m *PersistenceManager) StopAndSave() error {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
	return m.SaveAll()
}

func (m *PersistenceManager) emitError(err error) {
	if err == nil {
		return
	}
	if m.onError != nil {
		m.onError(err)
	}
}
