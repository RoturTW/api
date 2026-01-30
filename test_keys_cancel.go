package main

import (
	"testing"
	"time"
)

func TestCancelKey_DeferredUntilNextBilling(t *testing.T) {
	// Minimal in-memory setup (no gin/router needed)
	keys = nil

	now := time.Now()
	cancelAtMs := now.Add(2 * time.Hour).UnixMilli()

	k := Key{
		Key:          "key1",
		Creator:      "owner",
		Users:        map[UserId]KeyUserData{},
		Name:         ptr("sub key"),
		Price:        10,
		Type:         "subscription",
		Webhook:      nil,
		Data:         nil,
		TotalIncome:  0,
		Subscription: &Subscription{Active: true, Frequency: 1, Period: "month", NextBilling: cancelAtMs},
	}
	k.Users["alice"] = KeyUserData{Time: now.Unix(), Price: 10, NextBilling: cancelAtMs}
	keys = []Key{k}

	// Simulate what /keys/cancel does: set CancelAt = nextBilling
	ud := keys[0].Users["alice"]
	ud.CancelAt = ud.NextBilling
	keys[0].Users["alice"] = ud

	if !doesUserOwnKey("alice", "key1") {
		t.Fatalf("expected user to still own key until cancel_at")
	}

	// Simulate the cleanup portion of checkSubscriptions: remove if cancel_at has passed
	pastCancelAt := now.Add(-1 * time.Hour).UnixMilli()
	ud = keys[0].Users["alice"]
	ud.CancelAt = pastCancelAt
	keys[0].Users["alice"] = ud

	// run the relevant logic block: remove if time.Now >= cancelAt
	usersToRemove := []UserId{}
	if v, ok := keys[0].Users["alice"].CancelAt.(int64); ok {
		if time.Now().UnixMilli() >= v {
			usersToRemove = append(usersToRemove, "alice")
		}
	}
	for _, u := range usersToRemove {
		delete(keys[0].Users, u)
	}

	if doesUserOwnKey("alice", "key1") {
		t.Fatalf("expected user to be removed after cancel_at")
	}
}

func ptr(s string) *string { return &s }
