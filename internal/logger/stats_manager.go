package logger

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// StatsManager handles periodic statistics logging
type StatsManager struct {
	interval time.Duration
	stopCh   chan struct{}
	wg       sync.WaitGroup
	counters map[string]*atomic.Uint64
	mu       sync.Mutex
}

// NewStatsManager creates a new stats manager
// intervalSec: Logging interval in seconds
func NewStatsManager(intervalSec int) *StatsManager {
	if intervalSec <= 0 {
		intervalSec = 30
	}
	return &StatsManager{
		interval: time.Duration(intervalSec) * time.Second,
		stopCh:   make(chan struct{}),
		counters: make(map[string]*atomic.Uint64),
	}
}

// RegisterCounter registers a new counter and returns a pointer to it for fast updates.
// The returned atomic.Uint64 can be used to increment the counter directly.
func (sm *StatsManager) RegisterCounter(name string) *atomic.Uint64 {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, exists := sm.counters[name]; !exists {
		sm.counters[name] = &atomic.Uint64{}
	}
	return sm.counters[name]
}

// Start begins the periodic logging loop
func (sm *StatsManager) Start() {
	sm.wg.Add(1)
	go sm.run()
}

// Stop stops the logging loop
func (sm *StatsManager) Stop() {
	close(sm.stopCh)
	sm.wg.Wait()
}

func (sm *StatsManager) run() {
	defer sm.wg.Done()
	ticker := time.NewTicker(sm.interval)
	defer ticker.Stop()

	// Track previous values to calculate diffs
	prevValues := make(map[string]uint64)

	// Initialize prevValues
	sm.mu.Lock()
	for name, counter := range sm.counters {
		prevValues[name] = counter.Load()
	}
	sm.mu.Unlock()

	for {
		select {
		case <-sm.stopCh:
			return
		case <-ticker.C:
			sm.logStats(prevValues)
		}
	}
}

func (sm *StatsManager) logStats(prevValues map[string]uint64) {
	sm.mu.Lock()
	// Create a stable list of names for consistent ordering
	var names []string
	for name := range sm.counters {
		names = append(names, name)
	}
	sm.mu.Unlock()
	sort.Strings(names)

	var parts []string
	intervalSec := sm.interval.Seconds()

	for _, name := range names {
		counter := sm.counters[name]
		current := counter.Load()
		prev := prevValues[name]
		diff := current - prev
		prevValues[name] = current // Update for next tick

		// Calculate rate (per second)
		rate := float64(diff) / intervalSec

		// Format: Name: Total (+Diff, Rate/s)
		// e.g. Forwarded: 10050 (+50, 1.6/s)
		part := fmt.Sprintf("%s: %d (+%d, %.1f/s)", name, current, diff, rate)
		parts = append(parts, part)
	}

	if len(parts) > 0 {
		Info("[STATS] %s", strings.Join(parts, " | "))
	}
}
