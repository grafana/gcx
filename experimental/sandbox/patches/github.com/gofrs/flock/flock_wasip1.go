//go:build wasip1

package flock

// wasip1 has no file locks, and upstream reports ErrUnsupported, which makes
// every gcx config write fail. Each sandbox instance is a single process, so
// a lock only it can see is enough; runs that share a Home are not locked
// against each other (see sandbox.Invocation.Home).

func (f *Flock) Lock() error { return f.set(true, false) }

func (f *Flock) RLock() error { return f.set(false, true) }

func (f *Flock) Unlock() error { return f.set(false, false) }

func (f *Flock) TryLock() (bool, error) { return true, f.Lock() }

func (f *Flock) TryRLock() (bool, error) { return true, f.RLock() }

func (f *Flock) set(l, r bool) error {
	f.m.Lock()
	defer f.m.Unlock()
	f.l, f.r = l, r
	return nil
}
