package main

import (
	"testing"
	"time"
)

func TestCooldown_FirstCallAllowed(t *testing.T) {
	c := NewCooldown(2 * time.Minute)
	if !c.Try(time.Unix(1000, 0)) {
		t.Fatal("first Try should be allowed")
	}
}

func TestCooldown_SecondCallWithinWindowBlocked(t *testing.T) {
	c := NewCooldown(2 * time.Minute)
	c.Try(time.Unix(1000, 0))
	if c.Try(time.Unix(1060, 0)) {
		t.Fatal("Try within window should be blocked")
	}
}

func TestCooldown_CallAfterWindowAllowed(t *testing.T) {
	c := NewCooldown(2 * time.Minute)
	c.Try(time.Unix(1000, 0))
	if !c.Try(time.Unix(1121, 0)) {
		t.Fatal("Try after window should be allowed")
	}
}

func TestCooldown_RemainingZeroWhenIdle(t *testing.T) {
	c := NewCooldown(2 * time.Minute)
	if got := c.Remaining(time.Unix(1000, 0)); got != 0 {
		t.Fatalf("Remaining on fresh cooldown = %v, want 0", got)
	}
}

func TestCooldown_RemainingDecreases(t *testing.T) {
	c := NewCooldown(2 * time.Minute)
	c.Try(time.Unix(1000, 0))
	got := c.Remaining(time.Unix(1030, 0))
	want := 90 * time.Second
	if got != want {
		t.Fatalf("Remaining = %v, want %v", got, want)
	}
}
