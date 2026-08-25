package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kimnt93/gorouter/internal/llm"
)

type Scope string

const (
	ScopeKey    Scope = "key"
	ScopeTenant Scope = "tenant"
	ScopeGlobal Scope = "global"
)

type Config struct {
	Enabled       bool
	TTL           time.Duration
	Scope         Scope
	MaxEntryBytes int
	MaxTotalBytes int64
}

func DefaultConfig() Config {
	return Config{
		Enabled:       true,
		TTL:           24 * time.Hour,
		Scope:         ScopeKey,
		MaxEntryBytes: 1 << 20,
		MaxTotalBytes: 256 << 20,
	}
}

type Entry struct {
	Status      int
	ContentType string
	Body        []byte
	Stream      bool
	PromptTok   int64
	Completion  int64
}

type Stats struct {
	Entries   int64   `json:"entries"`
	Bytes     int64   `json:"bytes"`
	Hits      uint64  `json:"hits"`
	Misses    uint64  `json:"misses"`
	Stores    uint64  `json:"stores"`
	Evictions uint64  `json:"evictions"`
	HitRatio  float64 `json:"hit_ratio"`
}

type item struct {
	key     string
	entry   *Entry
	expires int64
	size    int
}

type Cache struct {
	cfg    Config
	mu     sync.Mutex
	items  map[string]*listNode
	lru    *linkedList
	bytes  int64
	hits   atomic.Uint64
	misses atomic.Uint64
	stores atomic.Uint64
	evict  atomic.Uint64
	stop   chan struct{}
}

func New(cfg Config) *Cache {
	if cfg.MaxEntryBytes <= 0 {
		cfg.MaxEntryBytes = 1 << 20
	}
	if cfg.MaxTotalBytes <= 0 {
		cfg.MaxTotalBytes = 256 << 20
	}
	if cfg.TTL <= 0 {
		cfg.TTL = time.Hour
	}
	c := &Cache{cfg: cfg, items: map[string]*listNode{}, lru: &linkedList{}, stop: make(chan struct{})}
	go c.janitor()
	return c
}

func (c *Cache) Close() { close(c.stop) }

func (c *Cache) janitor() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case now := <-t.C:
			c.evictExpired(now.Unix())
		}
	}
}

func (c *Cache) evictExpired(now int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, n := range c.items {
		if n.it.expires <= now {
			c.removeLocked(n)
		}
	}
}

func (c *Cache) scopeValue(keyScope, tenantID string) string {
	switch c.cfg.Scope {
	case ScopeGlobal:
		return "*"
	case ScopeTenant:
		return tenantID
	default:
		return keyScope
	}
}

func BuildKey(scopeID, model string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(scopeID))
	h.Write([]byte{0})
	h.Write([]byte(model))
	h.Write([]byte{0})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

func (c *Cache) Lookup(apiKeyID, tenantID, model string, rawBody []byte) (*Entry, bool) {
	if !c.cfg.Enabled {
		c.misses.Add(1)
		return nil, false
	}
	k := BuildKey(c.scopeValue(apiKeyID, tenantID), model, rawBody)
	c.mu.Lock()
	n, ok := c.items[k]
	if !ok {
		c.mu.Unlock()
		c.misses.Add(1)
		return nil, false
	}
	if n.it.expires <= time.Now().Unix() {
		c.removeLocked(n)
		c.mu.Unlock()
		c.misses.Add(1)
		return nil, false
	}
	c.lru.moveToFront(n)
	e := n.it.entry
	c.mu.Unlock()
	c.hits.Add(1)
	return e, true
}

func (c *Cache) Store(apiKeyID, tenantID, model string, rawBody []byte, e *Entry) bool {
	if !c.cfg.Enabled || len(e.Body) > c.cfg.MaxEntryBytes {
		return false
	}
	k := BuildKey(c.scopeValue(apiKeyID, tenantID), model, rawBody)
	now := time.Now()
	it := &item{key: k, entry: e, expires: now.Add(c.cfg.TTL).Unix(), size: len(e.Body) + 128}
	c.mu.Lock()
	if n, ok := c.items[k]; ok {
		c.bytes -= int64(n.it.size)
		n.it = it
		c.lru.moveToFront(n)
	} else {
		n := &listNode{it: it}
		c.items[k] = n
		c.lru.pushFront(n)
	}
	c.bytes += int64(it.size)
	for c.bytes > c.cfg.MaxTotalBytes && c.lru.len() > 1 {
		back := c.lru.back()
		if back == nil {
			break
		}
		c.removeLocked(back)
		c.evict.Add(1)
	}
	c.mu.Unlock()
	c.stores.Add(1)
	return true
}

func (c *Cache) removeLocked(n *listNode) {
	c.bytes -= int64(n.it.size)
	c.lru.remove(n)
	delete(c.items, n.it.key)
}

func (c *Cache) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = map[string]*listNode{}
	c.lru = &linkedList{}
	c.bytes = 0
}

func (c *Cache) Stats() Stats {
	c.mu.Lock()
	entries, bytes := int64(len(c.items)), c.bytes
	c.mu.Unlock()
	hits, misses := c.hits.Load(), c.misses.Load()
	var ratio float64
	if total := hits + misses; total > 0 {
		ratio = float64(hits) / float64(total)
	}
	return Stats{Entries: entries, Bytes: bytes, Hits: hits, Misses: misses, Stores: c.stores.Load(), Evictions: c.evict.Load(), HitRatio: ratio}
}

// Deterministic reports whether a request is safe to serve from / store in
// the prompt cache: greedy sampling parameters and no tool definitions.
func Deterministic(req *llm.ChatRequest) bool {
	if req.Temperature != nil && *req.Temperature != 0 {
		return false
	}
	if req.TopP != nil && *req.TopP != 1 {
		return false
	}
	if req.N != nil && *req.N != 1 {
		return false
	}
	if req.FrequencyPenalty != nil && *req.FrequencyPenalty != 0 {
		return false
	}
	if req.PresencePenalty != nil && *req.PresencePenalty != 0 {
		return false
	}
	if len(req.Tools) > 0 {
		return false
	}
	return true
}

type listNode struct {
	it   *item
	prev *listNode
	next *listNode
}

type linkedList struct {
	head *listNode
	tail *listNode
	n    int
}

func (l *linkedList) pushFront(n *listNode) {
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

func (l *linkedList) moveToFront(n *listNode) {
	if l.head == n {
		return
	}
	l.remove(n)
	l.pushFront(n)
}

func (l *linkedList) remove(n *listNode) {
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

func (l *linkedList) back() *listNode { return l.tail }

func (l *linkedList) len() int { return l.n }
