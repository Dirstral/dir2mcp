package index

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type fakePersistIndex struct {
	loadCalls int32
	saveCalls int32
	loadErr   error
	saveErr   error
}

func (f *fakePersistIndex) Add(label uint64, vector []float32) error {
	_ = label
	_ = vector
	return nil
}

func (f *fakePersistIndex) Search(vector []float32, k int) ([]uint64, []float32, error) {
	_ = vector
	_ = k
	return nil, nil, nil
}

func (f *fakePersistIndex) Save(path string) error {
	_ = path
	atomic.AddInt32(&f.saveCalls, 1)
	return f.saveErr
}

func (f *fakePersistIndex) Load(path string) error {
	_ = path
	atomic.AddInt32(&f.loadCalls, 1)
	return f.loadErr
}

func (f *fakePersistIndex) Close() error { return nil }

func TestPersistenceManager_LoadAndSaveAll(t *testing.T) {
	i1 := &fakePersistIndex{}
	i2 := &fakePersistIndex{}
	pm := NewPersistenceManager([]IndexedFile{
		{Name: "text", Path: "text.idx", Index: i1},
		{Name: "code", Path: "code.idx", Index: i2},
	}, time.Second, nil)

	if err := pm.LoadAll(context.Background()); err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if err := pm.SaveAll(); err != nil {
		t.Fatalf("SaveAll failed: %v", err)
	}

	if atomic.LoadInt32(&i1.loadCalls) != 1 || atomic.LoadInt32(&i2.loadCalls) != 1 {
		t.Fatalf("unexpected load calls: i1=%d i2=%d", i1.loadCalls, i2.loadCalls)
	}
	if atomic.LoadInt32(&i1.saveCalls) != 1 || atomic.LoadInt32(&i2.saveCalls) != 1 {
		t.Fatalf("unexpected save calls: i1=%d i2=%d", i1.saveCalls, i2.saveCalls)
	}
}

func TestPersistenceManager_AutoSaveAndStop(t *testing.T) {
	i1 := &fakePersistIndex{}
	pm := NewPersistenceManager([]IndexedFile{
		{Name: "text", Path: "text.idx", Index: i1},
	}, 20*time.Millisecond, nil)

	pm.Start(context.Background())
	time.Sleep(60 * time.Millisecond)
	if err := pm.StopAndSave(); err != nil {
		t.Fatalf("StopAndSave failed: %v", err)
	}

	if atomic.LoadInt32(&i1.saveCalls) < 2 {
		t.Fatalf("expected at least 2 save calls (tick + final), got %d", i1.saveCalls)
	}
}
