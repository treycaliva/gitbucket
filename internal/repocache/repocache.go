// Package repocache bounds the disk (on Cloud Run, tmpfs-backed memory) used by
// materialized bare repositories under LOCAL_REPOS_ROOT.
//
// Each Git request materializes a repo at <root>/<owner>/<repo>.git and, before
// this package, nothing ever removed it — a busy instance touching many or large
// repos grew /tmp without bound until the instance OOMed. The Manager evicts the
// least-recently-used repos back down under a byte budget.
//
// Eviction is safe because GCS + Firestore are the source of truth: deleting a
// local bare repo just forces the next request to re-materialize it (the missing
// last_sync_timestamp makes needsSync true). Two guards prevent deleting a repo
// out from under active work:
//
//   - In-process pins: a request Pins a repo for its whole lifetime, so an
//     in-flight clone/push is never evicted on this instance.
//   - A caller-supplied lock guard (backed by the per-repo Firestore lock) is
//     acquired before deletion, so a push in progress on any instance blocks
//     eviction of that repo.
//
// A grace period additionally shields repos touched very recently by fast read
// paths that only Touch (browse) rather than Pin.
package repocache

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// LockGuard attempts to take the per-repo exclusive lock. On success it returns
// a release func and true; on contention or error it returns false and the repo
// is skipped for this sweep. The api layer supplies one backed by Firestore;
// tests supply a trivial one.
type LockGuard func(owner, repo string) (release func(), ok bool)

// Manager tracks access times and pins for materialized repos and evicts the
// coldest ones when total on-disk size exceeds maxBytes.
type Manager struct {
	root     string
	maxBytes int64
	grace    time.Duration

	mu     sync.Mutex
	pins   map[string]int
	access map[string]time.Time

	sweeping int32
}

// New returns a Manager rooted at localReposRoot with the given byte budget.
// maxBytes <= 0 disables eviction (Enabled reports false).
func New(localReposRoot string, maxBytes int64) *Manager {
	return &Manager{
		root:     localReposRoot,
		maxBytes: maxBytes,
		grace:    5 * time.Minute,
		pins:     make(map[string]int),
		access:   make(map[string]time.Time),
	}
}

// Enabled reports whether eviction is active. A nil Manager is disabled, so all
// call sites can invoke methods unconditionally.
func (m *Manager) Enabled() bool { return m != nil && m.maxBytes > 0 && m.root != "" }

func key(owner, repo string) string {
	return strings.ToLower(owner) + "/" + strings.ToLower(repo)
}

// dir is the on-disk bare repo path, matching the layout used across the api
// package: <root>/<owner-lower>/<repo-lower>.git.
func (m *Manager) dir(owner, repo string) string {
	return filepath.Join(m.root, strings.ToLower(owner), strings.ToLower(repo)+".git")
}

// Pin marks a repo as in active use on this instance and refreshes its access
// time. Every Pin must be paired with an Unpin (defer). No-op when disabled.
func (m *Manager) Pin(owner, repo string) {
	if !m.Enabled() {
		return
	}
	k := key(owner, repo)
	m.mu.Lock()
	m.pins[k]++
	m.access[k] = time.Now()
	m.mu.Unlock()
}

// Unpin releases a Pin. No-op when disabled.
func (m *Manager) Unpin(owner, repo string) {
	if !m.Enabled() {
		return
	}
	k := key(owner, repo)
	m.mu.Lock()
	if m.pins[k] > 1 {
		m.pins[k]--
	} else {
		delete(m.pins, k)
	}
	m.mu.Unlock()
}

// Touch records access from a short-lived path (e.g. browse) that does not hold
// a pin. No-op when disabled.
func (m *Manager) Touch(owner, repo string) {
	if !m.Enabled() {
		return
	}
	k := key(owner, repo)
	m.mu.Lock()
	m.access[k] = time.Now()
	m.mu.Unlock()
}

type candidate struct {
	owner, repo string
	dir         string
	size        int64
	access      time.Time
}

// Sweep evicts least-recently-used repos until total size is within budget.
// At most one sweep runs at a time per Manager; concurrent calls return
// immediately. Returns the number of bytes freed. Safe to call on a disabled
// or nil Manager (returns 0).
func (m *Manager) Sweep(ctx context.Context, guard LockGuard) int64 {
	if !m.Enabled() {
		return 0
	}
	if !atomic.CompareAndSwapInt32(&m.sweeping, 0, 1) {
		return 0
	}
	defer atomic.StoreInt32(&m.sweeping, 0)

	cands, total := m.scan()
	if total <= m.maxBytes {
		return 0
	}

	// Coldest first.
	sort.Slice(cands, func(i, j int) bool { return cands[i].access.Before(cands[j].access) })

	now := time.Now()
	var freed int64
	for _, c := range cands {
		if total-freed <= m.maxBytes {
			break
		}
		if ctx.Err() != nil {
			break
		}
		k := key(c.owner, c.repo)

		m.mu.Lock()
		pinned := m.pins[k] > 0
		recent := now.Sub(c.access) < m.grace
		m.mu.Unlock()
		if pinned || recent {
			continue
		}

		release, ok := guard(c.owner, c.repo)
		if !ok {
			continue // a writer holds the lock; leave this repo alone
		}
		err := os.RemoveAll(c.dir)
		release()
		if err != nil {
			continue
		}
		m.mu.Lock()
		delete(m.access, k)
		m.mu.Unlock()
		freed += c.size
	}
	return freed
}

// scan enumerates <root>/<owner>/<repo>.git directories with their sizes and
// last-known access times (falling back to directory mtime for repos this
// process never recorded, e.g. left over from a prior instance).
func (m *Manager) scan() ([]candidate, int64) {
	owners, err := os.ReadDir(m.root)
	if err != nil {
		return nil, 0
	}
	m.mu.Lock()
	accessSnapshot := make(map[string]time.Time, len(m.access))
	for k, v := range m.access {
		accessSnapshot[k] = v
	}
	m.mu.Unlock()

	var cands []candidate
	var total int64
	for _, o := range owners {
		if !o.IsDir() {
			continue
		}
		owner := o.Name()
		ownerDir := filepath.Join(m.root, owner)
		repos, err := os.ReadDir(ownerDir)
		if err != nil {
			continue
		}
		for _, rd := range repos {
			if !rd.IsDir() || !strings.HasSuffix(rd.Name(), ".git") {
				continue
			}
			repo := strings.TrimSuffix(rd.Name(), ".git")
			dir := filepath.Join(ownerDir, rd.Name())
			size := dirSize(dir)
			total += size

			k := key(owner, repo)
			acc, ok := accessSnapshot[k]
			if !ok {
				if info, err := os.Stat(dir); err == nil {
					acc = info.ModTime()
				}
			}
			cands = append(cands, candidate{owner: owner, repo: repo, dir: dir, size: size, access: acc})
		}
	}
	return cands, total
}

func dirSize(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}
