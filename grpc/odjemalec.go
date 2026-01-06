// client.go
package main

import (
	"context"
	"fmt"
	"log"

	pb "4a.si/razpravljalnica/grpc/protobufRazpravljalnica"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// Client wraps gRPC connection and JWT
type Client struct {
	conn  *grpc.ClientConn
	api   pb.MessageBoardClient
	token string
}

func ClientMain(url string) {
	// --- Client for An ---
	an, err := NewClient(url)
	if err != nil {
		panic(err)
	}
	defer an.Close()

	// --- Client for Bar ---
	bar, err := NewClient(url)
	if err != nil {
		panic(err)
	}
	defer bar.Close()

	anID := an.CreateUser("Anton")
	barID := bar.CreateUser("Barbara")

	an.Login(anID)
	bar.Login(barID)

	topicID := an.CreateTopic("chatting")

	fmt.Println("\n--- Posting messages ---")
	aMsgID := an.PostMessage(topicID, "i love swans")
	bMsgID := bar.PostMessage(topicID, "me too")

	fmt.Println("\n--- A likes B's message")
	an.LikeMessage(topicID, bMsgID)

	fmt.Println("\n--- A updates their message ---")
	an.UpdateMessage(topicID, aMsgID, "im hungry")

}

// NewClient creates a new gRPC client
func NewClient(addr string) (*Client, error) {
	conn, err := grpc.Dial(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	return &Client{
		conn: conn,
		api:  pb.NewMessageBoardClient(conn),
	}, nil
}

// Close connection
func (c *Client) Close() {
	c.conn.Close()
}

// ctx returns a context with JWT properly attached for gRPC calls
func (c *Client) ctx() context.Context {
	md := metadata.New(nil)

	// Only include authorization if we have token
	if c.token != "" {
		md.Set("authorization", "Bearer "+c.token)
	}

	return metadata.NewOutgoingContext(context.Background(), md)
}

// CreateUser calls CreateUser RPC (allowlisted, no JWT needed)
func (c *Client) CreateUser(name string) int64 {
	res, err := c.api.CreateUser(context.Background(), &pb.CreateUserRequest{Name: name})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Created user:", res)
	return res.Id
}

// Login calls Login RPC and stores JWT (allowlisted, no JWT needed)
func (c *Client) Login(userID int64) {
	res, err := c.api.Login(context.Background(), &pb.LoginRequest{UserId: userID})
	if err != nil {
		log.Fatal(err)
	}
	c.token = res.Token
	fmt.Println("Logged in, JWT acquired")
}

// CreateTopic calls CreateTopic RPC (JWT required)
func (c *Client) CreateTopic(name string) int64 {
	res, err := c.api.CreateTopic(c.ctx(), &pb.CreateTopicRequest{Name: name})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Created topic:", res)
	return res.Id
}

// PostMessage calls PostMessage RPC (JWT required)
func (c *Client) PostMessage(topicID int64, text string) int64 {
	res, err := c.api.PostMessage(c.ctx(), &pb.PostMessageRequest{
		TopicId: topicID,
		Text:    text,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Posted message:", res)
	return res.Id
}

// LikeMessage calls LikeMessage RPC (JWT required)
func (c *Client) LikeMessage(topicID, messageID int64) {
	res, err := c.api.LikeMessage(c.ctx(), &pb.LikeMessageRequest{
		TopicId:   topicID,
		MessageId: messageID,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Liked message:", res)
}

// UpdateMessage calls UpdateMessage RPC (JWT required)
func (c *Client) UpdateMessage(topicID, messageID int64, text string) {
	res, err := c.api.UpdateMessage(c.ctx(), &pb.UpdateMessageRequest{
		TopicId:   topicID,
		MessageId: messageID,
		Text:      text,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Updated message:", res)
}

// DeleteMessage calls DeleteMessage RPC (JWT required)
func (c *Client) DeleteMessage(messageID int64) {
	_, err := c.api.DeleteMessage(c.ctx(), &pb.DeleteMessageRequest{
		MessageId: messageID,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Deleted message")
}
