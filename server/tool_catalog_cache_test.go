package server

import (
	"testing"

	"github.com/RestXtra/RestXtraAI/db"
)

func TestToolCatalogCacheReloadsOnlyAfterInvalidation(t *testing.T) {
	var cache toolCatalogCache
	loads := 0
	load := func() ([]*db.Tool, error) {
		loads++
		return []*db.Tool{{Key: "tool"}}, nil
	}

	first, err := cache.get(load)
	if err != nil || len(first) != 1 || loads != 1 {
		t.Fatalf("first load rows=%v loads=%d err=%v", first, loads, err)
	}
	second, err := cache.get(load)
	if err != nil || len(second) != 1 || loads != 1 {
		t.Fatalf("cache hit rows=%v loads=%d err=%v", second, loads, err)
	}

	cache.Invalidate()
	if _, err := cache.get(load); err != nil || loads != 2 {
		t.Fatalf("reload after invalidation loads=%d err=%v", loads, err)
	}
}
