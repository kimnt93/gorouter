package promptcache

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kimnt93/gorouter/pkg/chat"
)

type memoryNode struct {
	it   *memoryItem
	prev *memoryNode
	next *memoryNode
}

type memoryItem struct {
	key     string
	entry   *chat.CacheEntry
	expires int64
	size    int
}

type memoryList struct {
	head *memoryNode
	tail *memoryNode
	n    int
}

func (l *memoryList) pushFront(n *memoryNode) {
	n.next = l.head
	n.prev = nil
	if l.head != nil {
		l.head.prev = n
	}
	l.head = n
	if l.tail == nil {
		l.tail = n
	}
	l.n++
}

func (l *memoryList) remove(n *memoryNode) {
	if n.prev != nil {
		n.prev.next = n.next
	} else if l.head == n {
		l.head = n.next
	}
	if n.next != nil {
		n.next.prev = n.prev
	} else if l.tail == n {
		l.tail = n.prev
	}
	n.prev, n.next = nil, nil
	l.n--
}

func (l *memoryList) moveToFront(n *memoryNode) {
	if l.head == n {
		return
	}
	l.remove(n)
	l.pushFront(n)
}

type Config struct {
	TTL           time.Duration
	Scope         string
	MaxEntryBytes int
	MaxTotalBytes int64
}

// Memory is a single-node LRU+TTL implementation of chat.PromptCache.
type Memory struct {
	cfg    Config
	mu     sync.Mutex
	items  map[string]*memoryNode
	lru    *memoryList
	bytes  int64
	hits   atomic.Uint64
	misses atomic.Uint64
	stores atomic.Uint64
	evict  atomic.Uint64
	stop   chan struct{}
}

func NewMemory(cfg Config) *Memory {
	if cfg.MaxEntryBytes <= 0 {
		cfg.MaxEntryBytes = 1 << 20
	}
	if cfg.MaxTotalBytes <= 0 {
		cfg.MaxTotalBytes = 256 << 20
	}
	if cfg.TTL <= 0 {
		cfg.TTL = time.Hour
	}
	m := &Memory{cfg: cfg, items: map[string]*memoryNode{}, lru: &memoryList{}, stop: make(chan struct{})}
	go m.janitor()
	return m
}

func (m *Memory) Close() { close(m.stop) }

func (m *Memory) janitor() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case now := <-t.C:
			m.evictExpired(now.UnixNano())
		}
	}
}

func (m *Memory) evictExpired(now int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, n := range m.items {
		if n.it.expires <= now {
			m.removeLocked(n)
		}
	}
}

func ScopeID(scope, apiKeyID, tenantID string) string {
	switch scope {
	case chat.ScopeGlobal:
		return "*"
	case chat.ScopeTenant:
		return tenantID
	default:
		return apiKeyID
	}
}

func BuildKey(scopeID, model string, body []byte) string {
	body = CanonicalBody(body)
	h := sha256.New()
	h.Write([]byte(scopeID))
	h.Write([]byte{0})
	h.Write([]byte(model))
	h.Write([]byte{0})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// CanonicalBody makes semantically identical JSON requests share a key while
// retaining a deterministic fallback for malformed input.
func CanonicalBody(body []byte) []byte {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return body
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return body
	}
	return canonical
}

func (m *Memory) Lookup(apiKeyID, tenantID, model string, body []byte) (*chat.CacheEntry, bool) {
	k := BuildKey(ScopeID(m.cfg.Scope, apiKeyID, tenantID), model, body)
	m.mu.Lock()
	n, ok := m.items[k]
	if !ok {
		m.mu.Unlock()
		m.misses.Add(1)
		return nil, false
	}
	if n.it.expires <= time.Now().UnixNano() {
		m.removeLocked(n)
		m.mu.Unlock()
		m.misses.Add(1)
		return nil, false
	}
	m.lru.moveToFront(n)
	e := n.it.entry
	m.mu.Unlock()
	m.hits.Add(1)
	return e, true
}

func (m *Memory) Store(apiKeyID, tenantID, model string, body []byte, e *chat.CacheEntry) bool {
	if len(e.Body) > m.cfg.MaxEntryBytes {
		return false
	}
	k := BuildKey(ScopeID(m.cfg.Scope, apiKeyID, tenantID), model, body)
	now := time.Now()
	it := &memoryItem{key: k, entry: e, expires: now.Add(m.cfg.TTL).UnixNano(), size: len(e.Body) + 128}
	m.mu.Lock()
	if n, ok := m.items[k]; ok {
		m.bytes -= int64(n.it.size)
		n.it = it
		m.lru.moveToFront(n)
	} else {
		n := &memoryNode{it: it}
		m.items[k] = n
		m.lru.pushFront(n)
	}
	m.bytes += int64(it.size)
	for m.bytes > m.cfg.MaxTotalBytes && m.lru.n > 1 {
		back := m.lru.tail
		if back == nil {
			break
		}
		m.removeLocked(back)
		m.evict.Add(1)
	}
	m.mu.Unlock()
	m.stores.Add(1)
	return true
}

func (m *Memory) removeLocked(n *memoryNode) {
	m.bytes -= int64(n.it.size)
	m.lru.remove(n)
	delete(m.items, n.it.key)
}

func (m *Memory) Flush() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = map[string]*memoryNode{}
	m.lru = &memoryList{}
	m.bytes = 0
}

func (m *Memory) Stats() chat.CacheStats {
	m.mu.Lock()
	entries, bytes := int64(len(m.items)), m.bytes
	m.mu.Unlock()
	hits, misses := m.hits.Load(), m.misses.Load()
	var ratio float64
	if total := hits + misses; total > 0 {
		ratio = float64(hits) / float64(total)
	}
	return chat.CacheStats{Entries: entries, Bytes: bytes, Hits: hits, Misses: misses, Stores: m.stores.Load(), Evictions: m.evict.Load(), HitRatio: ratio}
}
