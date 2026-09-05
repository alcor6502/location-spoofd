package main

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// bssidCache remembers every access point any client has asked about, so replies can carry the
// whole neighbourhood the way Apple's do. Shared across devices in the same place, persisted one
// BSSID per line, most recently seen first.
type bssidCache struct {
	mu   sync.Mutex
	path string
	seen map[string]time.Time
	max  int
}

func newBSSIDCache(dir string, max int) *bssidCache {
	c := &bssidCache{path: filepath.Join(dir, "bssids"), seen: map[string]time.Time{}, max: max}
	if f, err := os.Open(c.path); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		t := time.Now()
		for sc.Scan() {
			if b := strings.TrimSpace(sc.Text()); b != "" {
				c.seen[b] = t
				t = t.Add(-time.Second) // keep file order as recency
			}
		}
	}
	return c
}

// Learn records the access points of one request and persists the cache.
func (c *bssidCache) Learn(bssids []string) {
	if len(bssids) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for _, b := range bssids {
		c.seen[b] = now
	}
	list := c.sorted()
	if len(list) > c.max {
		for _, b := range list[c.max:] {
			delete(c.seen, b)
		}
		list = list[:c.max]
	}
	_ = os.WriteFile(c.path, []byte(strings.Join(list, "\n")+"\n"), 0o644)
}

// Neighbours returns the known access points, most recent first.
func (c *bssidCache) Neighbours() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sorted()
}

func (c *bssidCache) sorted() []string {
	list := make([]string, 0, len(c.seen))
	for b := range c.seen {
		list = append(list, b)
	}
	sort.Slice(list, func(i, j int) bool {
		ti, tj := c.seen[list[i]], c.seen[list[j]]
		if ti.Equal(tj) {
			return list[i] < list[j]
		}
		return ti.After(tj)
	})
	return list
}
