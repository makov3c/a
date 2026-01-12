//go:build integration
package main
import (
	"testing"
	"time"
)
func TestLikeMessage(t *testing.T) {
	cpAddr := "localhost:9800"
	alice, err := NewClientCP([]string{cpAddr})
	if err != nil {
		t.Fatalf("failed to create alice client: %v", err)
	}
	defer alice.Close()
	bob, err := NewClientCP([]string{cpAddr})
	if err != nil {
		t.Fatalf("failed to create bob client: %v", err)
	}
	defer bob.Close()
	aliceID, err := alice.CreateUser("test_alice")
	if err != nil {
		t.Fatalf("failed to create alice user: %v", err)
	}
	bobID, err := bob.CreateUser("test_bob")
	if err != nil {
		t.Fatalf("failed to create bob user: %v", err)
	}
	if err := alice.Login(aliceID); err != nil {
		t.Fatalf("alice login failed: %v", err)
	}
	if err := bob.Login(bobID); err != nil {
		t.Fatalf("bob login failed: %v", err)
	}
	topicID, err := alice.CreateTopic("like-test-topic")
	if err != nil {
		t.Fatalf("failed to create topic: %v", err)
	}
	msgID, err := bob.PostMessage(topicID, "please like this message")
	if err != nil {
		t.Fatalf("failed to post message: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := alice.LikeMessage(topicID, msgID); err != nil {
		t.Fatalf("first LikeMessage failed, expected success: %v", err)
	}
	if err := alice.LikeMessage(topicID, msgID); err == nil {
		t.Fatalf("second LikeMessage succeeded, expected error")
	}
}
