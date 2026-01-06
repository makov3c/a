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
	a, err := NewClient(url)
	if err != nil {
		panic(err)
	}
	defer a.Close()

	b, err := NewClient(url)
	if err != nil {
		panic(err)
	}
	defer b.Close()

	anID := a.CreateUser("Anton")
	barID := b.CreateUser("Barbara")

	a.Login(anID)
	b.Login(barID)

	topicID := a.CreateTopic("chatting")

	fmt.Println("\n--- Posting messages ---")
	aMsgID := a.PostMessage(topicID, "i love this")
	bMsgID := b.PostMessage(topicID, "me too")

	fmt.Println("\n--- A likes B's message")
	a.LikeMessage(topicID, bMsgID)

	fmt.Println("\n--- A updates their message ---")
	a.UpdateMessage(topicID, aMsgID, "im hungry")

	// Request subscription node for this topic
	//subRes, err := a.GetSubscriptionNode([]int64{123})
	_, err = a.GetSubscriptionNode([]int64{topicID})
	if err != nil {
		log.Fatal("Failed to get subscription node:", err)
	}
	fmt.Println("Subscribed to topic:", topicID)
}

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

func (c *Client) Close() {
	c.conn.Close()
}

func (c *Client) ctx() context.Context {
	md := metadata.New(nil)

	// Only include authorization if we have token
	if c.token != "" {
		md.Set("authorization", "Bearer "+c.token)
	}

	return metadata.NewOutgoingContext(context.Background(), md)
}

func (c *Client) CreateUser(name string) int64 {
	res, err := c.api.CreateUser(context.Background(), &pb.CreateUserRequest{Name: name})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Created user:", res)
	return res.Id
}

func (c *Client) Login(userID int64) {
	res, err := c.api.Login(context.Background(), &pb.LoginRequest{UserId: userID})
	if err != nil {
		log.Fatal(err)
	}
	c.token = res.Token
	fmt.Println("Logged in, JWT acquired")
}

func (c *Client) CreateTopic(name string) int64 {
	res, err := c.api.CreateTopic(c.ctx(), &pb.CreateTopicRequest{Name: name})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Created topic:", res)
	return res.Id
}

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

func (c *Client) DeleteMessage(messageID int64) {
	_, err := c.api.DeleteMessage(c.ctx(), &pb.DeleteMessageRequest{
		MessageId: messageID,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Deleted message")
}

func (c *Client) GetSubscriptionNode(topicIDs []int64) (*pb.SubscriptionNodeResponse, error) {

	req := &pb.SubscriptionNodeRequest{
		TopicId: topicIDs,
	}

	res, err := c.api.GetSubscriptionNode(c.ctx(), req)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Received subscription node: ID=%s, Address=%s\n", res.Node.NodeId, res.Node.Address)
	fmt.Printf("Subscribe token: %s\n", res.SubscribeToken)

	return res, nil
}
