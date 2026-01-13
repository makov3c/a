//go:build integration
package odjemalec
import (
	"testing"
	"time"
	pb "4a.si/razpravljalnica/protobuf"
)
func getMessagesForTest (t *testing.T, c *Client, topicID int64) []*pb.Message {
	t.Helper()
	res, err := c.Tail.GetMessages(c.Ctx(), &pb.GetMessagesRequest{
		TopicId:       topicID,
		FromMessageId: 0,
		Limit:         100,
	})
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	return res.Messages
}
func TestUpdateMessage(t *testing.T) {
	controlPlaneAddr := "localhost:9800"
	client, err := NewClientCP([]string{controlPlaneAddr})
	if err != nil {
		t.Fatalf("NewClientCP failed: %v", err)
	}
	defer client.Close()
	userID, err := client.CreateUser("test-user-update")
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := client.Login(userID); err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	topicID, err := client.CreateTopic("update-message-test-topic")
	if err != nil {
		t.Fatalf("CreateTopic failed: %v", err)
	}
	originalText := "original message text"
	msgID, err := client.PostMessage(topicID, originalText)
	if err != nil {
		t.Fatalf("PostMessage failed: %v", err)
	}
	updatedText := "updated message text"
	if err := client.UpdateMessage(topicID, msgID, updatedText); err != nil {
		t.Fatalf("UpdateMessage failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	messages := getMessagesForTest(t, client, topicID)
	var found bool
	for _, m := range messages {
		if m.Id == msgID {
			found = true
			if m.Text != updatedText {
				t.Fatalf(
					"message text mismatch: expected %q, got %q",
					updatedText,
					m.Text,
				)
			}
		}
	}
	if !found {
		t.Fatalf("updated message with ID %d not found", msgID)
	}
}
