package promptcache

import "github.com/kimnt93/gorouter/pkg/chat"

// Noop keeps cache-disabled wiring nil-safe.
type Noop struct{}

func (Noop) Lookup(string, string, string, []byte) (*chat.CacheEntry, bool) { return nil, false }
func (Noop) Store(string, string, string, []byte, *chat.CacheEntry) bool    { return false }
func (Noop) Flush()                                                         {}
func (Noop) Stats() chat.CacheStats                                         { return chat.CacheStats{} }
func (Noop) Close()                                                         {}

var _ chat.PromptCache = Noop{}
