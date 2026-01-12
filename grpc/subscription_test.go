//go:build integration
package main
import (
	"context"
	"fmt"
	"log"
	"testing"
	"time"
	pb "4a.si/razpravljalnica/grpc/protobufRazpravljalnica"
)
func TestMessageSubscription(t *testing.T) {
	clientA, err := NewClientCP([]string{"localhost:9800"})
	if err != nil {
		t.Fatalf("Failed to create client A: %v", err)
	}
	defer clientA.Close()
	clientB, err := NewClientCP([]string{"localhost:9800"})
	if err != nil {
		t.Fatalf("Failed to create client B: %v", err)
	}
	defer clientB.Close()
	aID, err := clientA.CreateUser("Alice")
	if err != nil {
		t.Fatalf("CreateUser Alice failed: %v", err)
	}
	if err := clientA.Login(aID); err != nil {
		t.Fatalf("Login Alice failed: %v", err)
	}
	bID, err := clientB.CreateUser("Bob")
	if err != nil {
		t.Fatalf("CreateUser Bob failed: %v", err)
	}
	if err := clientB.Login(bID); err != nil {
		t.Fatalf("Login Bob failed: %v", err)
	}
	topicID, err := clientA.CreateTopic("TestTopic")
	if err != nil {
		t.Fatalf("CreateTopic failed: %v", err)
	}
	subRes, err := clientA.GetSubscriptionNode([]int64{topicID})
	if err != nil {
		t.Fatalf("GetSubscriptionNode failed: %v", err)
	}
	if subRes.Node == nil || subRes.SubscribeToken == "" {
		t.Fatalf("Subscription node or token missing")
	}
	fmt.Printf("Subscribed to node: %s with token: %s\n", subRes.Node.Address, subRes.SubscribeToken)
	subConn, err := NewClient(subRes.Node.Address)
	if err != nil {
		t.Fatalf("Failed to connect to subscription node: %v", err)
	}
	defer subConn.Close()
	ctx, cancel := context.WithCancel(subConn.ctx())
	defer cancel()
	stream, err := subConn.api.SubscribeTopic(ctx, &pb.SubscribeTopicRequest{
		TopicId:       []int64{topicID},
		SubscribeToken: subRes.SubscribeToken,
		FromMessageId: 0,
	})
	if err != nil {
		t.Fatalf("SubscribeTopic failed: %v", err)
	}
	go func() {
		time.Sleep(500 * time.Millisecond)
		msgs := []string{"Hello Alice", "seks?", "Testing subscription"}
		for _, m := range msgs {
			if _, err := clientB.PostMessage(topicID, m); err != nil {
				log.Printf("Failed to post message: %v", err)
			}
		}
	}()
	received := []string{}
	timeout := time.After(5 * time.Second)
	prev := ""
	for len(received) < 3 {
		select {
		case <-timeout:
			t.Fatalf("Timeout waiting for subscription messages. Received: %v", received)
		default:
			ev, err := stream.Recv()
			if err != nil {
				t.Fatalf("Error receiving from subscription: %v", err)
			}
			if prev == ev.Message.Text {
				break
			}
			fmt.Printf("Received message via subscription: %s\n", ev.Message.Text)
			received = append(received, ev.Message.Text)
			prev = ev.Message.Text
		}
	}
	expected := []string{"Hello Alice", "seks?", "Testing subscription"}
	for i, msg := range expected {
		if received[i] != msg {
			t.Errorf("Expected message %q but got %q", msg, received[i])
		}
	}
}

