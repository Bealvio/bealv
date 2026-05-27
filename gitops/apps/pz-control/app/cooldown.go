package main

import (
	"sync"
	"time"
)

type Cooldown struct {
	mu       sync.Mutex
	last     time.Time
	duration time.Duration
}

func NewCooldown(d time.Duration) *Cooldown {
	return &Cooldown{duration: d}
}

// Try records the action and returns true if it is permitted.
// Returns false if a previous Try happened within the cooldown window.
func (c *Cooldown) Try(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.last.IsZero() && now.Sub(c.last) < c.duration {
		return false
	}
	c.last = now
	return true
}

// Remaining returns the duration until the cooldown expires, or 0 if ready.
func (c *Cooldown) Remaining(now time.Time) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last.IsZero() {
		return 0
	}
	r := c.duration - now.Sub(c.last)
	if r < 0 {
		return 0
	}
	return r
}
