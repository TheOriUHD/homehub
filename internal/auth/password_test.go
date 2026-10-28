package auth

import (
	"testing"
	"time"
)

// With every hash slot taken, a verification must wait rather than allocate
// another 64 MiB — that wait is the memory cap on a login flood.
func TestVerifyPasswordWaitsForAHashSlot(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	for range cap(hashSlots) {
		hashSlots <- struct{}{}
	}
	done := make(chan bool, 1)
	go func() {
		ok, _ := VerifyPassword("correct horse battery", hash)
		done <- ok
	}()
	select {
	case <-done:
		t.Fatal("verification ran although every hash slot was taken")
	case <-time.After(150 * time.Millisecond):
	}
	<-hashSlots // free one slot
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("password did not verify")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("verification never got the freed slot")
	}
	for range cap(hashSlots) - 1 {
		<-hashSlots
	}
}
