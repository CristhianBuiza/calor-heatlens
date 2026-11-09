package server

import (
	"container/list"
	"context"
	"strconv"
	"sync"
	"time"

	"calor/api/internal/store"
)

// CacheEntry holds an encoded JSON response and its lazily built gzip form.
type CacheEntry struct {
	Raw     []byte
	gzOnce  sync.Once
	gz      []byte
	expires time.Time
	key     string
}

func (e *CacheEntry) Gzip() []byte {
	e.gzOnce.Do(func() { e.gz = gzipBytes(e.Raw) })
	return e.gz
}

// Cache is a small TTL + LRU cache bounded by entry count and total bytes.
type Cache struct {
	ttl      time.Duration
	max      int
	maxBytes int
	mu       sync.Mutex
	ll       *list.List
	items    map[string]*list.Element
	bytes    int
}

func NewCache(ttl time.Duration, maxEntries int) *Cache {
	return &Cache{ttl: ttl, max: maxEntries, maxBytes: 16 << 20, ll: list.New(), items: map[string]*list.Element{}}
}

func (c *Cache) Get(key string) (*CacheEntry, bool) {
	if c.ttl <= 0 {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	e := el.Value.(*CacheEntry)
	if time.Now().After(e.expires) {
		c.remove(el)
		return nil, false
	}
	c.ll.MoveToFront(el)
	return e, true
}

func (c *Cache) Put(key string, raw []byte) *CacheEntry {
	e := &CacheEntry{Raw: raw, key: key, expires: time.Now().Add(c.ttl)}
	if c.ttl <= 0 || len(raw) > c.maxBytes/8 {
		return e
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.remove(el)
	}
	c.items[key] = c.ll.PushFront(e)
	c.bytes += len(raw)
	for c.ll.Len() > c.max || c.bytes > c.maxBytes {
		c.remove(c.ll.Back())
	}
	return e
}

func (c *Cache) remove(el *list.Element) {
	e := el.Value.(*CacheEntry)
	c.ll.Remove(el)
	delete(c.items, e.key)
	c.bytes -= len(e.Raw)
}

// siteRegistry keeps registered sites in memory so ingest does not hit the
// database per request; generations change whenever a site's config or
// screenshots change, invalidating cached responses for that site.
type siteRegistry struct {
	st     *store.Store
	mu     sync.RWMutex
	loaded bool
	sites  map[string]store.Site
	gens   map[string]int
}

func (s *siteRegistry) load(ctx context.Context) {
	sites, err := s.st.ListSites(ctx)
	if err != nil {
		return
	}
	m := make(map[string]store.Site, len(sites))
	for _, site := range sites {
		m[site.ID] = site
	}
	s.sites = m
	s.loaded = true
}

func (s *siteRegistry) get(ctx context.Context, id string) (store.Site, bool) {
	s.mu.RLock()
	if s.loaded {
		site, ok := s.sites[id]
		s.mu.RUnlock()
		return site, ok
	}
	s.mu.RUnlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		s.load(ctx)
	}
	site, ok := s.sites[id]
	return site, ok
}

// invalidate reloads the registry and bumps the generation of id and "*".
func (s *siteRegistry) invalidate(ctx context.Context, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load(ctx)
	if s.gens == nil {
		s.gens = map[string]int{}
	}
	s.gens[id]++
	s.gens["*"]++
}

func (s *siteRegistry) generation(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return id + ":" + strconv.Itoa(s.gens[id]) + ":" + strconv.Itoa(s.gens["*"])
}
