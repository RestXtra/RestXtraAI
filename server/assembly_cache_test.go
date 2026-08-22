package server

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Autumn-27/norma/skill"
	"github.com/RestXtra/RestXtraAI/db"
)

func TestAgentAssemblyCacheInvalidatesCatalogsIndependently(t *testing.T) {
	var cache agentAssemblyCache
	skillLoads := 0
	mcpLoads := 0
	loadSkills := func() ([]skill.Skill, error) {
		skillLoads++
		return []skill.Skill{{Name: "recon"}}, nil
	}
	loadMCPs := func() ([]*db.MCPServer, error) {
		mcpLoads++
		return []*db.MCPServer{{ID: 1, Name: "browser"}}, nil
	}

	for range 2 {
		if rows, err := cache.skills(loadSkills); err != nil || len(rows) != 1 {
			t.Fatalf("skills rows=%v err=%v", rows, err)
		}
		if rows, err := cache.mcps(loadMCPs); err != nil || len(rows) != 1 {
			t.Fatalf("mcps rows=%v err=%v", rows, err)
		}
	}
	if skillLoads != 1 || mcpLoads != 1 {
		t.Fatalf("cache hits skillLoads=%d mcpLoads=%d", skillLoads, mcpLoads)
	}

	cache.InvalidateSkills()
	if _, err := cache.skills(loadSkills); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.mcps(loadMCPs); err != nil {
		t.Fatal(err)
	}
	if skillLoads != 2 || mcpLoads != 1 {
		t.Fatalf("skill invalidation skillLoads=%d mcpLoads=%d", skillLoads, mcpLoads)
	}

	cache.InvalidateMCPs()
	if _, err := cache.mcps(loadMCPs); err != nil {
		t.Fatal(err)
	}
	if skillLoads != 2 || mcpLoads != 2 {
		t.Fatalf("mcp invalidation skillLoads=%d mcpLoads=%d", skillLoads, mcpLoads)
	}
}

func TestAssemblyValueCacheRetriesFailedLoad(t *testing.T) {
	var cache assemblyValueCache[int]
	loads := 0
	load := func() ([]int, error) {
		loads++
		if loads == 1 {
			return nil, errors.New("temporary failure")
		}
		return []int{42}, nil
	}

	if _, err := cache.get(load); err == nil {
		t.Fatal("expected first load to fail")
	}
	rows, err := cache.get(load)
	if err != nil || len(rows) != 1 || rows[0] != 42 || loads != 2 {
		t.Fatalf("retry rows=%v loads=%d err=%v", rows, loads, err)
	}
}

func TestAssemblyValueCacheSingleFlightsColdLoad(t *testing.T) {
	var cache assemblyValueCache[int]
	var loads atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	load := func() ([]int, error) {
		if loads.Add(1) == 1 {
			close(started)
		}
		<-release
		return []int{7}, nil
	}

	const callers = 24
	var wg sync.WaitGroup
	wg.Add(callers)
	errs := make(chan error, callers)
	for range callers {
		go func() {
			defer wg.Done()
			rows, err := cache.get(load)
			if err != nil {
				errs <- err
				return
			}
			if len(rows) != 1 || rows[0] != 7 {
				errs <- errors.New("unexpected cached rows")
			}
		}()
	}
	<-started
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if got := loads.Load(); got != 1 {
		t.Fatalf("cold loader called %d times, want 1", got)
	}
}
