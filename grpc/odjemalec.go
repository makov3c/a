package main

import (
	"context"
	"fmt"
	"log"

	pb "4a.si/razpravljalnica/grpc/protobufRazpravljalnica"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ControlPlaneClient struct {
	conn *grpc.ClientConn
	api pb.ControlPlaneClient
	token string
}
func NewControlPlaneClient (addr string, s2s_psk string) (*ControlPlaneClient, error) {
	conn, err := grpc.Dial(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	log.Printf("newcontrolplaneclient called with ", addr, " ", s2s_psk, "\n")
	return &ControlPlaneClient{
		conn: conn,
		api:  pb.NewControlPlaneClient(conn),
		token: s2s_psk,
	}, nil
}
func (c *ControlPlaneClient) Close() {
	c.conn.Close()
}
func (c *ControlPlaneClient) ctx () context.Context {
	md := metadata.New(nil)
	if c.token != "" {
		md.Set("authorization", "Bearer "+c.token)
	}
	return metadata.NewOutgoingContext(context.Background(), md)
}
func (c *ControlPlaneClient) GetClusterState () (string, string, error) {
	res, err := c.api.GetClusterState(context.Background(), &emptypb.Empty{})
	if err != nil {
		return "", "", err
	}
	return res.Head.Address, res.Tail.Address, nil
}
func (c *ControlPlaneClient) ReportNotifyError (slave_url string) error {
	_, err := c.api.ReportNotifyError(c.ctx(), &pb.NotifyErrorReport{Slave: slave_url})
	if err != nil {
		return err
	}
	return nil
}
func (c *ControlPlaneClient) ServerAvailable (url string) error {
	_, err := c.api.ServerAvailable(c.ctx(), &pb.NodeInfo{NodeId: url, Address: url})
	if err != nil {
		return err
	}
	return nil
}

// Client wraps gRPC connection and JWT
type Client struct {
	conn  *grpc.ClientConn
	tailconn *grpc.ClientConn
	api	pb.MessageBoardClient // head
	tail	pb.MessageBoardClient // tail
	token string
	cpurl string
	cpc *ControlPlaneClient
	userid int64
}

func ClientMain(url string, uName string) {
	a, err := NewClientCP(url)
	if err != nil {
		panic(err)
	}
	defer a.Close()

	b, err := NewClientCP(url)
	if err != nil {
		panic(err)
	}
	defer b.Close()

	aID, err := a.CreateUser("anton")
	if err != nil {
		log.Fatal("CreateUser: %v", err)
	}
	bID, err := b.CreateUser("Barbara")
	if err != nil {
		log.Fatal("CreateUser: %v", err)
	}

	a.Login(aID)
	b.Login(bID)

	topicID := a.CreateTopic("chatting")

	fmt.Println("\n--- Posting messages ---")
	aMsgID := a.PostMessage(topicID, "i love this")
	bMsgID := b.PostMessage(topicID, "me too")

	b.PostMessage(topicID, "doing PS hw")
	b.PostMessage(topicID, "during matlab exercises")
	b.PostMessage(topicID, "writing code")
	a.PostMessage(topicID, "all alone")
	a.PostMessage(topicID, "and also writing silly messages")
	bMsgID2 := b.PostMessage(topicID, "so i can test")
	b.PostMessage(topicID, "if function")
	b.PostMessage(topicID, "get messages something something")
	b.PostMessage(topicID, "actually works")

	fmt.Println("\n--- A likes B's message")
	err = a.LikeMessage(topicID, bMsgID)
	if err != nil {
		panic(err)
	}
	err = a.LikeMessage(topicID, bMsgID)
	if err == nil {
		panic("pričakoval sem napako")
	}
	a.LikeMessage(topicID, bMsgID2)

	fmt.Println("\n--- A updates their message ---")
	a.UpdateMessage(topicID, aMsgID, "im hungry")

	// Request subscription node for this topic
	//subRes, err := a.GetSubscriptionNode([]int64{123})
	_, err = a.GetSubscriptionNode([]int64{topicID})
	if err != nil {
		log.Fatal("Failed to get subscription node:", err)
	}
	fmt.Println("Subscribed to topic:", topicID)

	a.ListTopics()

	a.GetUsers()

	c, err := NewClientCP(url)
	if err != nil {
		panic(err)
	}
	defer c.Close()

	cID, err := c.CreateUser("Cecilija")
	if err != nil {
		log.Fatal("CreateUser: %v", err)
	}
	c.Login(cID)

	c.GetUsers()

	b.GetMessages(topicID, 0, 10)
	fmt.Println("\n --- \n")
	b.GetMessages(topicID, bMsgID2, 4)
}
func NewClientCP(addr string) (*Client, error) {
	cpc, err := NewControlPlaneClient(addr, "odjemalec")
	if err != nil {
		return nil, err
	}
	head, tail, err := cpc.GetClusterState()
	if err != nil {
		return nil, err
	}
	headconn, err := grpc.Dial(
		head,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	tailconn, err := grpc.Dial(
		tail,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	c := &Client{
		conn: headconn,
		tailconn: tailconn,
		api:  pb.NewMessageBoardClient(headconn),
		tail:  pb.NewMessageBoardClient(tailconn),
		cpc: cpc,
		cpurl: addr,
	}
	return c, nil
}
func NewClient(addr string) (*Client, error) {
	// log.Printf("NewClient called with ", addr)
	conn, err := grpc.Dial(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	c := &Client{
		conn: conn,
		api:  pb.NewMessageBoardClient(conn),
	}
	return c, nil
}
func (c *Client) Close() {
	c.conn.Close()
}
func (c *Client) ctx() context.Context {
	md := metadata.New(nil)
	if c.token != "" {
		md.Set("authorization", "Bearer "+c.token)
	}
	return metadata.NewOutgoingContext(context.Background(), md)
}
func (c *Client) CreateUser(name string) (int64, error) {
	res, err := c.api.CreateUser(context.Background(), &pb.CreateUserRequest{Name: name})
	if err != nil {
		return 0, err
	}
	return res.Id, nil
}
func (c *Client) Login(userID int64) error {
	res, err := c.api.Login(context.Background(), &pb.LoginRequest{UserId: userID})
	if err != nil {
		return err
	}
	c.token = res.Token
	c.userid = userID
	return nil
}

func (c *Client) CreateTopic(name string) int64 {
	res, err := c.api.CreateTopic(c.ctx(), &pb.CreateTopicRequest{Name: name})
	if err != nil {
		log.Fatal(err)
	}
	return res.Id
}

func (c *Client) AddUser(user *pb.User) error {
	_, err := c.api.AddUser(c.ctx(), user)
	return err
}
func (c *Client) AddMessage(message *pb.Message) error {
	_, err := c.api.AddMessage(c.ctx(), message)
	return err
}
func (c *Client) AddTopic(topic *pb.Topic) error {
	_, err := c.api.AddTopic(c.ctx(), topic)
	return err
}
func (c *Client) AddLike(like *pb.LikeMessageRequest) error {
	_, err := c.api.AddLike(c.ctx(), like)
	return err
}
func (c *Client) PostMessage(topicID int64, text string) int64 {
	res, err := c.api.PostMessage(c.ctx(), &pb.PostMessageRequest{
		TopicId: topicID,
		Text:    text,
	})
	if err != nil {
		log.Fatal(err)
	}
	// fmt.Println("Posted message:", res)
	return res.Id
}

func (c *Client) LikeMessage(topicID, messageID int64) error {
	_, err := c.api.LikeMessage(c.ctx(), &pb.LikeMessageRequest{
		TopicId:   topicID,
		MessageId: messageID,
	})
	if err != nil {
		return err
	}
	return nil
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
		UserId: c.userid,
		MessageId: messageID,
	})
	if err != nil {
		log.Fatal(err)
	}
}

func (c *Client) GetSubscriptionNode(topicIDs []int64) (*pb.SubscriptionNodeResponse, error) {

	req := &pb.SubscriptionNodeRequest{
		TopicId: topicIDs,
	}

	res, err := c.tail.GetSubscriptionNode(c.ctx(), req)
	if err != nil {
		return nil, err
	}

	// fmt.Printf("Received subscription node: ID=%s, Address=%s\n", res.Node.NodeId, res.Node.Address)
	// fmt.Printf("Subscribe token: %s\n", res.SubscribeToken)

	return res, nil
}

func (c *Client) ListTopics() {

	res, err := c.tail.ListTopics(c.ctx(), &emptypb.Empty{})
	if err != nil {
		log.Fatal(err)
	}
	for _, t := range res.Topics {
		fmt.Printf("Topic ID: %d, Name: %s\n", t.Id, t.Name)
	}
}

func (c *Client) GetUsers() (map[int64]string, error) {
	res, err := c.tail.GetUsers(c.ctx(), &emptypb.Empty{})
	if err != nil {
		log.Fatal(err)
	}
	r := make(map[int64]string)
	for _, u := range res.Users {
		// fmt.Printf("User ID: %d, User name: %s\n", u.Id, u.Name)
		r[u.Id] = u.Name
	}
	return r, nil
}

func (c *Client) GetMessages(topicID int64, fromMessageID int64, limit int32) {

	res, err := c.tail.GetMessages(c.ctx(), &pb.GetMessagesRequest{
		TopicId:       topicID,
		FromMessageId: fromMessageID,
		Limit:         limit,
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, m := range res.Messages {
		fmt.Printf(
			"Message ID: %d | Topic: %d | User: %d | Likes: %d | %s\n",
			m.Id,
			m.TopicId,
			m.UserId,
			m.Likes,
			m.Text,
		)
	}
}
