package odjemalec
import (
	"context"
	"fmt"
	"log"
	"time"
	pb "4a.si/razpravljalnica/protobuf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
)
type ControlPlaneClient struct {
	conn *grpc.ClientConn
	Api pb.ControlPlaneClient
	Token string
}
func NewControlPlaneClient (addr string, s2s_psk string) (*ControlPlaneClient, error) {
	conn, err := grpc.Dial(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	// log.Printf("newcontrolplaneclient called with ", addr, " ", s2s_psk, "\n")
	return &ControlPlaneClient{
		conn: conn,
		Api:  pb.NewControlPlaneClient(conn),
		Token: s2s_psk,
	}, nil
}
func (c *ControlPlaneClient) Close() {
	c.conn.Close()
}
func (c *ControlPlaneClient) Ctx () context.Context {
	md := metadata.New(nil)
	if c.Token != "" {
		md.Set("authorization", "Bearer "+c.Token)
	}
	return metadata.NewOutgoingContext(context.Background(), md)
}
func (c *ControlPlaneClient) GetClusterState () (string, string, error) {
	res, err := c.Api.GetClusterState(context.Background(), &emptypb.Empty{})
	if err != nil {
		return "", "", err
	}
	return res.Head.Address, res.Tail.Address, nil
}
func (c *ControlPlaneClient) ReportNotifyError (slave_url string) error {
	_, err := c.Api.ReportNotifyError(c.Ctx(), &pb.NotifyErrorReport{Slave: slave_url})
	if err != nil {
		return err
	}
	return nil
}
func (c *ControlPlaneClient) ServerAvailable (url string) error {
	_, err := c.Api.ServerAvailable(c.Ctx(), &pb.NodeInfo{NodeId: url, Address: url})
	if err != nil {
		return err
	}
	return nil
}
func (c *ControlPlaneClient) GetRaftLeader (deadline time.Time) (*pb.ControlPlaneInfo, error) { // deadline je recimo time.Now().Add(time.Second())
	RETRY:
	ctx, _ := context.WithDeadline(c.Ctx(), deadline)
	res, err := c.Api.GetRaftLeader(ctx, &emptypb.Empty{})
	if err != nil {
		if deadline.Sub(time.Now()).Seconds() > 0 {
			return nil, err
		}
		st, ok := status.FromError(err)
		if ok && st.Code() == codes.Unavailable {
			goto RETRY // meh loh bi dal kkšn dilej sam eh sej mamo network latency za delay FIXME
		}
		return nil, err
	}
	return res, nil
}
// Client wraps gRPC connection and JWT
type Client struct {
	conn  *grpc.ClientConn
	tailconn *grpc.ClientConn
	Api	pb.MessageBoardClient // head
	Tail	pb.MessageBoardClient // tail
	Token string
	cpurl string
	cpc *ControlPlaneClient
	Userid int64
	Cplanes []string // filled by NewClientCP, use in future calls to NewClientCP
}

func ClientMain(url string, uName string) {
	a, err := NewClientCP([]string{url})
	if err != nil {
		panic(err)
	}
	defer a.Close()
	b, err := NewClientCP([]string{url})
	if err != nil {
		panic(err)
	}
	defer b.Close()

	aID, err := a.CreateUser("anton")
	if err != nil {
		panic(err)
	}
	bID, err := b.CreateUser("Barbara")
	if err != nil {
		panic(err)
	}
	a.Login(aID)
	b.Login(bID)
	topicID, _ := a.CreateTopic("chatting")

	fmt.Println("\n--- Posting messages ---")
	aMsgID, _ := a.PostMessage(topicID, "i love this")
	bMsgID, _ := b.PostMessage(topicID, "me too")

	b.PostMessage(topicID, "doing PS hw")
	b.PostMessage(topicID, "during matlab exercises")
	b.PostMessage(topicID, "writing code")
	a.PostMessage(topicID, "all alone")
	a.PostMessage(topicID, "and also writing silly messages")
	bMsgID2, _ := b.PostMessage(topicID, "so i can test")
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

	c, err := NewClientCP([]string{url})
	if err != nil {
		panic(err)
	}
	defer c.Close()

	cID, err := c.CreateUser("Cecilija")
	if err != nil {
		panic(err)
	}
	c.Login(cID)

	c.GetUsers()

	b.GetMessages(topicID, 0, 10)
	fmt.Println("\n --- ")
	b.GetMessages(topicID, bMsgID2, 4)
}
func NewClientCP(addrs []string) (*Client, error) {
	var cpc *ControlPlaneClient
	var err error
	for _, addr := range addrs {
		cpc, err = NewControlPlaneClient(addr, "odjemalec")
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	resx, err := cpc.GetRaftLeader(time.Now().Add(time.Second))
	if err != nil {
		return nil, err
	}
	cpc.Close()
	leaderurl := resx.ControlplaneAddress
	cpc, err = NewControlPlaneClient(leaderurl, "odjemalec")
	if err != nil {
		return nil, err
	}
	res, err := cpc.Api.GetControlPlanes(cpc.Ctx(), &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	cplanes := []string{}
	for _, cpinfo := range res.ControlPlanes {
		cplanes = append(cplanes, cpinfo.ControlplaneAddress)
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
		Api:  pb.NewMessageBoardClient(headconn),
		Tail:  pb.NewMessageBoardClient(tailconn),
		cpc: cpc,
		cpurl: leaderurl,
		Cplanes: cplanes,
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
		Api:  pb.NewMessageBoardClient(conn),
	}
	return c, nil
}
func (c *Client) Close() {
	c.conn.Close()
}
func (c *Client) Ctx() context.Context {
	md := metadata.New(nil)
	if c.Token != "" {
		md.Set("authorization", "Bearer "+c.Token)
	}
	return metadata.NewOutgoingContext(context.Background(), md)
}
func (c *Client) CreateUser(name string) (int64, error) {
	res, err := c.Api.CreateUser(context.Background(), &pb.CreateUserRequest{Name: name})
	if err != nil {
		return 0, err
	}
	return res.Id, nil
}
func (c *Client) Login (userID int64) error {
	res, err := c.Api.Login (context.Background(), &pb.LoginRequest{UserId: userID})
	if err != nil {
		return err
	}
	c.Token = res.Token
	c.Userid = userID
	return nil
}

func (c *Client) CreateTopic(name string) (int64, error) {
	res, err := c.Api.CreateTopic(c.Ctx(), &pb.CreateTopicRequest{Name: name})
	if err != nil {
		return 0, err
	}
	return res.Id, nil
}

func (c *Client) AddUser(user *pb.User) error {
	_, err := c.Api.AddUser(c.Ctx(), user)
	return err
}
func (c *Client) AddMessage(message *pb.Message) error {
	_, err := c.Api.AddMessage(c.Ctx(), message)
	return err
}
func (c *Client) AddTopic(topic *pb.Topic) error {
	_, err := c.Api.AddTopic(c.Ctx(), topic)
	return err
}
func (c *Client) AddLike(like *pb.LikeMessageRequest) error {
	_, err := c.Api.AddLike(c.Ctx(), like)
	return err
}
func (c *Client) PostMessage (topicID int64, text string) (int64, error) {
	ctx, _ := context.WithDeadline(c.Ctx(), time.Now().Add(time.Second))
	res, err := c.Api.PostMessage(ctx, &pb.PostMessageRequest{
		TopicId: topicID,
		Text:    text,
	})
	if err != nil {
		return 0, err
	}
	// fmt.Println("Posted message:", res)
	return res.Id, nil
}

func (c *Client) LikeMessage(topicID, messageID int64) error {
	_, err := c.Api.LikeMessage(c.Ctx(), &pb.LikeMessageRequest{
		TopicId:   topicID,
		MessageId: messageID,
	})
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) UpdateMessage(topicID, messageID int64, text string) error {
	_, err := c.Api.UpdateMessage(c.Ctx(), &pb.UpdateMessageRequest{
		TopicId:   topicID,
		MessageId: messageID,
		Text:      text,
	})
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) DeleteMessage (messageID int64) error {
	_, err := c.Api.DeleteMessage(c.Ctx(), &pb.DeleteMessageRequest{
		UserId: c.Userid,
		MessageId: messageID,
	})
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) GetSubscriptionNode(topicIDs []int64) (*pb.SubscriptionNodeResponse, error) {

	req := &pb.SubscriptionNodeRequest{
		TopicId: topicIDs,
	}

	res, err := c.Tail.GetSubscriptionNode(c.Ctx(), req)
	if err != nil {
		return nil, err
	}

	// fmt.Printf("Received subscription node: ID=%s, Address=%s\n", res.Node.NodeId, res.Node.Address)
	// fmt.Printf("Subscribe token: %s\n", res.SubscribeToken)

	return res, nil
}

func (c *Client) ListTopics () {

	res, err := c.Tail.ListTopics(c.Ctx(), &emptypb.Empty{})
	if err != nil {
		log.Fatal(err)
	}
	for _, t := range res.Topics {
		fmt.Printf("Topic ID: %d, Name: %s\n", t.Id, t.Name)
	}
}

func (c *Client) GetUsers() (map[int64]string, error) {
	res, err := c.Tail.GetUsers(c.Ctx(), &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	r := make(map[int64]string)
	for _, u := range res.Users {
		// fmt.Printf("User ID: %d, User name: %s\n", u.Id, u.Name)
		r[u.Id] = u.Name
	}
	return r, nil
}

func (c *Client) GetMessages (topicID int64, fromMessageID int64, limit int32) {

	res, err := c.Tail.GetMessages(c.Ctx(), &pb.GetMessagesRequest{
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
