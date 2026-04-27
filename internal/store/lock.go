package store

import "sync"

// pathLocks is a per-path mutex map. Singletons take Lock(key) for the
// duration of read-modify-write; collections take Lock(key/id) so two
// IDs in the same collection never block each other; streams don't take
// locks at all (their O_APPEND writes are inherently atomic for small
// payloads).
//
// Implemented as a sync.Map of *sync.Mutex — entries are created on
// demand and never evicted. The memory cost is ~24 bytes per distinct
// path; at ICP scale this is invisible. Eviction would add complexity
// (need to know when a mutex is safe to drop) for no real benefit.
type pathLocks struct {
	m sync.Map // path string -> *sync.Mutex
}

func newPathLocks() *pathLocks { return &pathLocks{} }

func (p *pathLocks) Lock(path string) func() {
	mu := p.get(path)
	mu.Lock()
	return mu.Unlock
}

func (p *pathLocks) get(path string) *sync.Mutex {
	if v, ok := p.m.Load(path); ok {
		return v.(*sync.Mutex)
	}
	mu := &sync.Mutex{}
	actual, _ := p.m.LoadOrStore(path, mu)
	return actual.(*sync.Mutex)
}
