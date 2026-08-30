package fleet

import (
	"fmt"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// CacheSizeForTest and FillCacheForTest let the package's own test see the table's size and age
// its entries — the two things eviction is about, and neither is worth exporting for production.
func CacheSizeForTest(c *Cache) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entry)
}

func FillCacheForTest(c *Cache, n int, stale bool) {
	for i := 0; i < n; i++ {
		sid := session.SessionID(fmt.Sprintf("s_%d_%v", i, stale))
		c.put(sid, cacheEntry{seq: 1, said: "something"})
		if stale {
			c.mu.Lock()
			e := c.entry[sid]
			e.touched = time.Now().Add(-2 * cacheKeep)
			c.entry[sid] = e
			c.mu.Unlock()
		}
	}
}
