// Komunikacija po protokolu gRPC
// strežnik

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pb "4a.si/razpravljalnica/grpc/protobufRazpravljalnica"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/golang-jwt/jwt/v5"
)

// TO DO: move to env variable
var jwtSecret = []byte("CHANGE_ME_SUPER_SECRET_KEY")

type Claims struct {
	UserID int64 `json:"user_id"`
	jwt.RegisteredClaims
}

type SubscriptionClaims struct {
	UserID   int64   `json:"user_id"`
	TopicIDs []int64 `json:"topic_ids"`
	jwt.RegisteredClaims
}

type User struct {
	ID   int64  `gorm:"primaryKey"`
	Name string `gorm:"not null"`
}

type Topic struct {
	ID   int64  `gorm:"primaryKey"`
	Name string `gorm:"not null"`
}

type Message struct {
	ID        int64     `gorm:"primaryKey"`
	TopicID   int64     `gorm:"not null;index"`
	UserID    int64     `gorm:"not null;index"`
	Text      string    `gorm:"not null"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	Likes     int32     `gorm:"default:0"`

	User  User  `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`
	Topic Topic `gorm:"foreignKey:TopicID;constraint:OnDelete:CASCADE"`
}

type MessageLike struct {
	MessageID int64 `gorm:"primaryKey"`
	UserID    int64 `gorm:"primaryKey"`

	// _ struct{} `gorm:"uniqueIndex:idx_message_user"` // (user can like a message only once)
}

type server struct {
	pb.UnimplementedMessageBoardServer

	db *gorm.DB
	// lastUserID   int64
	// mu_userCount sync.Mutex
	masters        []string // masters servers URLs. if len(s.masters) == 0, i am head node. updated on every FetchFrom.
	slaves         []string // slaves servers URLs, just subscribers
	controlplane   string   // control plane URL, "" for single server setup
	cpc            *ControlPlaneClient
	cpc_lock       sync.RWMutex
	s2s_psk        string
	naročniki      map[chan pb.MessageEvent]pb.SubscribeTopicRequest
	naročniki_lock sync.RWMutex
	event_št       atomic.Int64
	url            string // own url
}

func Server(url string, controlplane string, s2s_psk string, myurl string, dbfile string) {
	if dbfile == "" {
		dbfile = ":memory:"
	}
	db, err := gorm.Open(sqlite.Open(dbfile), &gorm.Config{})
	if err != nil {
		panic("failed to connect to the database")
	}

	db.Exec("PRAGMA foreign_keys = ON") // ker imava sqlite

	if err := db.AutoMigrate(&User{}, &Topic{}, &Message{}, &MessageLike{}); err != nil {
		panic("failed to migrate database")
	}

	s := &server{db: db, controlplane: controlplane, s2s_psk: s2s_psk, url: myurl, naročniki: make(map[chan pb.MessageEvent]pb.SubscribeTopicRequest)} // creates a new value of type server, assigns field db to the vasiable users_db (a *gorm DB)
	// pripravimo strežnik gRPC
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(logInterceptor(s), authInterceptor(s)),
	)

	pb.RegisterMessageBoardServer(grpcServer, s)

	// izpišemo ime strežnika
	hostName, err := os.Hostname()
	if err != nil {
		panic(err)
	}
	// odpremo vtičnico
	listener, err := net.Listen("tcp", url)
	if err != nil {
		panic(err)
	}
	fmt.Printf("gRPC server listening at hostname %v, url %v\n", hostName, url)
	var wg sync.WaitGroup
	wg.Add(1)
	s.cpc_lock.Lock()
	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			panic(err)
		}
		wg.Done()
	}()
	controlplanes := make(map[string]struct{})
	if controlplane != "" {
		controlplanes[controlplane] = struct{}{}
	}
	s.cpc = nil
	s.cpc_lock.Unlock()
	for {
		time.Sleep(time.Second) // TODO: do this in a loop, check cpc leader, call serveravailable every second (for raft)
		log.Printf("controlplanes: %v", controlplanes)
		if len(controlplanes) == 0 {
			log.Printf("no controlplanes, exiting loop")
			break
		}
		if s2s_psk == "" {
			log.Fatal("s2s_psk ne sme biti prazen niz")
		}
		var cpc *ControlPlaneClient
		cpc = nil
		for cpurl, _ := range controlplanes {
			var err error
			cpc, err = NewControlPlaneClient(cpurl, s2s_psk)
			if err != nil {
				panic(err)
			}
			res, err := cpc.GetRaftLeader(time.Now().Add(time.Second))
			if err != nil {
				st, ok := status.FromError(err)
				if ok && st.Code() == codes.FailedPrecondition {
					err = cpc.ServerAvailable(myurl)
					if err != nil {
						panic(err)
					}
					s.cpc_lock.Lock()
					s.cpc = cpc
					s.cpc_lock.Unlock()
					log.Printf("Controlplane does not use RAFT. Controlplane failure has undefined consequences for the behaviour of this strežnik.")
					goto NORAFT
				}
				//		NOT DELETING, SAME AS WE DON'T DELETE VOTERS I GUESS IDK IDK
				//		log.Printf("Deleting controlplane %v due to error: %v", cpurl, err)
				//		delete(controlplanes, cpurl)
				if len(controlplanes) == 0 {
					panic("lost connection to all controlplanes")
				}
				continue
			}
			log.Printf("healthcheck: obtainer raft leader: %v", res)
			cpc.Close()
			cpc, err = NewControlPlaneClient(res.ControlplaneAddress, s2s_psk)
			if err != nil {
				panic(err)
			}
			ctx, _ := context.WithDeadline(cpc.ctx(), time.Now().Add(time.Second))
			cps, err := cpc.api.GetControlPlanes(ctx, &emptypb.Empty{})
			if err != nil {
				log.Printf("SPORNO, kao leader ne more odgovoriti na getcontrolplanes: %v", err)
				cpc.Close()
				continue
			}
			for _, cpu := range cps.ControlPlanes {
				controlplanes[cpu.ControlplaneAddress] = struct{}{}
			}
			err = cpc.ServerAvailable(myurl)
			if err != nil {
				log.Printf("SPORNO, kao leader ne more odgovoriti na ServerAvailable: %v", err)
				cpc.Close()
				continue
			}
			break
		}
		if cpc == nil {
			log.Fatal("failed to establish connection to cpc") // this could be fixed in theory FIXME
		}
		s.cpc_lock.Lock()
		if s.cpc != nil {
			s.cpc.Close()
		}
		s.cpc = cpc
		s.cpc_lock.Unlock()
	}
NORAFT:
	wg.Wait()
}
func (s *server) FetchFrom(ctx context.Context, req *pb.FetchFromRequest) (*emptypb.Empty, error) {
	log.Printf("FetchFrom called")
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if userID != -1 {
		log.Printf("FetchFrom: wrong s2s authentication")
		return nil, status.Error(codes.Unauthenticated, "wrong s2s authentication")
	}
	log.Printf("FetchFrom handler: auth successful")
	if reflect.DeepEqual(s.masters, req.Masters) {
		return &emptypb.Empty{}, nil // noop, ne spreminja se master
	}
	for _, masterurl := range req.Masters {
		c, err := NewClient(masterurl)
		if err != nil {
			return nil, err
		}
		c.token = s.s2s_psk
		log.Printf("FetchFrom: created a client for %v", masterurl)
		defer c.Close()
		{
			res, err := c.api.GetUsers(c.ctx(), &emptypb.Empty{})
			if err != nil {
				return nil, err
			}
			for _, u := range res.Users {
				_, err := s.AddUser(ctx, u)
				if err != nil {
					return nil, err
				}
			}
		}
		{
			res, err := c.api.ListTopics(c.ctx(), &emptypb.Empty{})
			if err != nil {
				return nil, err
			}
			for _, t := range res.Topics {
				_, err := s.AddTopic(ctx, t)
				if err != nil {
					return nil, err
				}
			}
		}
		{
			res, err := c.api.GetLikes(c.ctx(), &emptypb.Empty{})
			if err != nil {
				return nil, err
			}
			for _, l := range res.Likes {
				_, err := s.AddLike(ctx, l)
				if err != nil {
					return nil, err
				}
			}
		}
		{
			res, err := c.api.GetMessages(c.ctx(), &pb.GetMessagesRequest{
				TopicId:       -1,
				FromMessageId: -1,
				Limit:         99999999,
			})
			if err != nil {
				return nil, err
			}
			for _, m := range res.Messages {
				_, err := s.AddMessage(ctx, m)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	s.masters = req.Masters
	return &emptypb.Empty{}, nil
}
func (s *server) SetSlaves(ctx context.Context, req *pb.SetSlavesRequest) (*emptypb.Empty, error) {
	log.Printf("SetSlaves called")
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if userID != -1 {
		log.Printf("SetSlaves: returning wrong s2s auth")
		return nil, status.Error(codes.Unauthenticated, "wrong s2s authentication")
	}
	s.slaves = req.Slaves
	log.Printf("SetSlaves set slaves, auth successful")
	return &emptypb.Empty{}, nil
}
func (s *server) s2sctx() context.Context {
	md := metadata.New(nil)
	md.Set("authorization", s.s2s_psk)
	return metadata.NewOutgoingContext(context.Background(), md)
}
func (s *server) handle_notify_err(err error, slave string) {
	// FIXME notify control plane
}
func (s *server) NotifySlavesUser(user *pb.User) {
	for _, slave := range s.slaves {
		client, err := NewClient(slave)
		if err != nil {
			s.handle_notify_err(err, slave)
			continue
		}
		client.token = s.s2s_psk
		err = client.AddUser(user)
		client.Close()
		if err != nil {
			s.handle_notify_err(err, slave)
		}
	}
}
func (s *server) NotifySlavesMessage(message *pb.Message) {
	for _, slave := range s.slaves {
		client, err := NewClient(slave)
		if err != nil {
			s.handle_notify_err(err, slave)
			continue
		}
		client.token = s.s2s_psk
		err = client.AddMessage(message)
		client.Close()
		if err != nil {
			s.handle_notify_err(err, slave)
		}
	}
	št_dogodka := s.event_št.Add(1)
	s.obvesti_naročnike(pb.MessageEvent{SequenceNumber: št_dogodka, Op: pb.OpType_OP_POST, Message: message, EventAt: timestamppb.Now()})
	št_dogodka = s.event_št.Add(1)
	s.obvesti_naročnike(pb.MessageEvent{SequenceNumber: št_dogodka, Op: pb.OpType_OP_LIKE, Message: message, EventAt: timestamppb.Now()})
	št_dogodka = s.event_št.Add(1)
	s.obvesti_naročnike(pb.MessageEvent{SequenceNumber: št_dogodka, Op: pb.OpType_OP_UPDATE, Message: message, EventAt: timestamppb.Now()})
}
func (s *server) NotifySlavesTopic(topic *pb.Topic) {
	for _, slave := range s.slaves {
		client, err := NewClient(slave)
		if err != nil {
			s.handle_notify_err(err, slave)
			continue
		}
		client.token = s.s2s_psk
		err = client.AddTopic(topic)
		client.Close()
		if err != nil {
			s.handle_notify_err(err, slave)
		}
	}
}
func (s *server) NotifySlavesLike(like *pb.LikeMessageRequest) {
	for _, slave := range s.slaves {
		client, err := NewClient(slave)
		if err != nil {
			s.handle_notify_err(err, slave)
			continue
		}
		client.token = s.s2s_psk
		err = client.AddLike(like)
		client.Close()
		if err != nil {
			s.handle_notify_err(err, slave)
		}
	}
}
func (s *server) AddUser(ctx context.Context, req *pb.User) (*emptypb.Empty, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if userID != -1 {
		return nil, status.Error(codes.Unauthenticated, "wrong s2s authentication")
	}
	user := &User{Name: req.Name, ID: req.Id}
	if err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{UpdateAll: true}).Create(user).Error; err != nil {
		return nil, err
	}
	s.NotifySlavesUser(req)
	return &emptypb.Empty{}, nil
}
func (s *server) AddMessage(ctx context.Context, req *pb.Message) (*emptypb.Empty, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if userID != -1 {
		return nil, status.Error(codes.Unauthenticated, "wrong s2s authentication")
	}
	message := &Message{ID: req.Id, TopicID: req.TopicId, UserID: req.UserId, Text: req.Text, CreatedAt: req.CreatedAt.AsTime(), Likes: req.Likes}
	if err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{UpdateAll: true}).Create(message).Error; err != nil {
		return nil, err
	}
	s.NotifySlavesMessage(req)
	return &emptypb.Empty{}, nil
}
func (s *server) AddLike(ctx context.Context, req *pb.LikeMessageRequest) (*emptypb.Empty, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if userID != -1 {
		return nil, status.Error(codes.Unauthenticated, "wrong s2s authentication")
	}
	like := &MessageLike{MessageID: req.MessageId, UserID: req.UserId}
	if err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{UpdateAll: true}).Create(like).Error; err != nil {
		return nil, err
	}
	s.NotifySlavesLike(req)
	return &emptypb.Empty{}, nil
}
func (s *server) AddTopic(ctx context.Context, req *pb.Topic) (*emptypb.Empty, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if userID != -1 {
		return nil, status.Error(codes.Unauthenticated, "wrong s2s authentication")
	}
	topic := &Topic{ID: req.Id, Name: req.Name}
	if err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{UpdateAll: true}).Create(topic).Error; err != nil {
		return nil, err
	}
	s.NotifySlavesTopic(req)
	return &emptypb.Empty{}, nil
}
func (s *server) CreateUser(ctx context.Context, req *pb.CreateUserRequest) (*pb.User, error) {
	if len(s.masters) != 0 {
		return nil, status.Error(codes.Internal, "write zahteva poslana na ne-head node v verigi")
	}
	user := &User{Name: req.Name}
	// WithContext - if client cancels the request the db operation is canceled too
	if err := s.db.WithContext(ctx).Create(user).Error; err != nil {
		return nil, err
	}
	userpb := &pb.User{Id: user.ID, Name: user.Name}
	s.NotifySlavesUser(userpb)
	return userpb, nil
}

func (s *server) CreateTopic(ctx context.Context, req *pb.CreateTopicRequest) (*pb.Topic, error) {
	topic := &Topic{Name: req.Name}
	if len(s.masters) != 0 {
		return nil, status.Error(codes.Internal, "write zahteva poslala na ne-head node v verigi")
	}
	if err := s.db.WithContext(ctx).Create(topic).Error; err != nil {
		return nil, err
	}

	pbtopic := &pb.Topic{Id: topic.ID, Name: topic.Name}
	s.NotifySlavesTopic(pbtopic)

	return pbtopic, nil

}

// --------------- ????
// TO DO: napiši teste(?) za slabe stvari (ko avtentikacija ne dela oz. uspešno dela in zavrne)
func generateJWT(userID int64) (string, error) {
	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)), // ???
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

func parseJWT(tokenStr string) (int64, error) {
	token, err := jwt.ParseWithClaims(
		tokenStr,
		&Claims{},
		func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, errors.New("unexpected signing method")
			}
			return jwtSecret, nil
		},
	)

	if err != nil || !token.Valid {
		return 0, errors.New("invalid token")
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return 0, errors.New("invalid claims")
	}

	return claims.UserID, nil
}
func logInterceptor(s *server) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		log.Printf("logInterceptor request %v %v", info, req)
		res, err := handler(ctx, req)
		log.Printf("logInterceptor response %v %v", res, err)
		return res, err
	}
}
func authInterceptor(s *server) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		switch info.FullMethod {
		case
			"/razpravljalnica.MessageBoard/Login",
			"/razpravljalnica.MessageBoard/CreateUser":
			return handler(ctx, req)
		}
		// everything below REQUIRES authentication
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}
		authHeader := md.Get("authorization")
		if len(authHeader) == 0 {
			return nil, status.Error(codes.Unauthenticated, fmt.Sprintf("missing authorization header in streznik.go authInterceptor info=%v req=%v", info.FullMethod, req))
		}
		parts := strings.SplitN(authHeader[0], " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			return nil, status.Error(codes.Unauthenticated, "invalid authorization format")
		}
		if parts[1] == s.s2s_psk {
			existingMD, _ := metadata.FromIncomingContext(ctx)
			existingMD = existingMD.Copy()
			existingMD.Set("user-id", "-1")
			ctx = metadata.NewIncomingContext(ctx, existingMD)
			return handler(ctx, req)
		}
		userID, err := parseJWT(parts[1])
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}

		existingMD, _ := metadata.FromIncomingContext(ctx)
		existingMD = existingMD.Copy()
		existingMD.Set("user-id", strconv.FormatInt(userID, 10))

		ctx = metadata.NewIncomingContext(ctx, existingMD)

		return handler(ctx, req)
	}
}

func userIDFromContext(ctx context.Context) (int64, error) {
	md, ok := metadata.FromIncomingContext(ctx) // md je tipa metadata.MD (map[string][]string)
	if !ok {
		return 0, status.Error(codes.Unauthenticated, "missing metadata")
	}

	values := md.Get("user-id")
	if len(values) == 0 {
		return 0, status.Error(codes.Unauthenticated, "user not authenticated")
	}
	id, err := strconv.ParseInt(values[0], 10, 64) // ker oblike values = []string{"67"}
	if err != nil {
		return 0, status.Error(codes.Unauthenticated, "invalid user id")
	}

	return id, nil
}

func (s *server) Ping(ctx context.Context, req *emptypb.Empty) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (s *server) Login( // poglej, explain, ...
	ctx context.Context,
	req *pb.LoginRequest,
) (*pb.LoginResponse, error) {

	// Check user exists
	var user User
	if err := s.db.WithContext(ctx).
		First(&user, req.UserId).
		Error; err != nil {

		return nil, status.Error(codes.NotFound, "user not found")
	}

	token, err := generateJWT(user.ID)
	if err != nil {
		return nil, status.Error(codes.Internal, "could not generate token")
	}
	log.Printf("Login: %v", req)
	return &pb.LoginResponse{
		Token: token,
	}, nil
}

func (s *server) PostMessage(ctx context.Context, req *pb.PostMessageRequest) (*pb.Message, error) {
	if len(s.masters) != 0 {
		return nil, status.Error(codes.Internal, "write zahteva poslala na ne-head node v verigi")
	}
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	message := &Message{
		TopicID: req.TopicId,
		UserID:  userID,
		Text:    req.Text,
	}

	if err := s.db.WithContext(ctx).Create(message).Error; err != nil {
		if errors.Is(err, gorm.ErrForeignKeyViolated) {
			return nil, status.Error(codes.FailedPrecondition, "user or topic doesn't exist")
		}

		return nil, err
	}

	pbmessage := &pb.Message{
		Id:        message.ID,
		TopicId:   message.TopicID,
		UserId:    message.UserID,
		Text:      message.Text,
		Likes:     message.Likes,
		CreatedAt: timestamppb.New(message.CreatedAt),
	}

	s.NotifySlavesMessage(pbmessage)

	return pbmessage, nil

}

func (s *server) UpdateMessage(ctx context.Context, req *pb.UpdateMessageRequest) (*pb.Message, error) {
	// authenticate user
	if len(s.masters) != 0 {
		return nil, status.Error(codes.Internal, "write zahteva poslala na ne-head node v verigi")
	}
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	result := s.db.WithContext(ctx).
		Model(&Message{}).
		Where("id = ? AND user_id = ? AND topic_id = ?", req.MessageId, userID, req.TopicId).
		Update("text", req.Text)

	if result.Error != nil {
		return nil, status.Error(codes.Internal, result.Error.Error())
	}

	if result.RowsAffected == 0 {
		return nil, status.Error(
			codes.PermissionDenied,
			"message not found or you are not the author",
		)
	}

	var msg Message
	if err := s.db.WithContext(ctx).
		First(&msg, req.MessageId).
		Error; err != nil {

		return nil, status.Error(codes.Internal, err.Error())
	}

	pbmessage := &pb.Message{
		Id:        msg.ID,
		TopicId:   msg.TopicID,
		UserId:    msg.UserID,
		Text:      msg.Text,
		Likes:     msg.Likes,
		CreatedAt: timestamppb.New(msg.CreatedAt),
	}

	s.NotifySlavesMessage(pbmessage)

	return pbmessage, nil
}

func (s *server) DeleteMessage(ctx context.Context, req *pb.DeleteMessageRequest) (*emptypb.Empty, error) {

	// authenticate user or server
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if len(s.masters) != 0 && userID != -1 {
		return nil, status.Error(codes.Internal, "write zahteva poslala na ne-head node v verigi")
	}
	result := s.db.WithContext(ctx).
		Where("id = ? AND (user_id = ? OR ? = -1)", req.MessageId, userID, userID).
		Delete(&Message{})

	if result.Error != nil {
		return nil, status.Error(codes.Internal, result.Error.Error())
	}

	if result.RowsAffected == 0 {
		return nil, status.Error(
			codes.PermissionDenied,
			"message not found or you are not the author",
		)
	}

	for _, slave := range s.slaves {
		client, err := NewClient(slave)
		if err != nil {
			s.handle_notify_err(err, slave)
			continue
		}
		client.token = s.s2s_psk
		err = client.DeleteMessage(req.MessageId)
		client.Close()
		if err != nil {
			s.handle_notify_err(err, slave)
		}
	}
	št_dogodka := s.event_št.Add(1)
	s.obvesti_naročnike(pb.MessageEvent{SequenceNumber: št_dogodka, Op: pb.OpType_OP_DELETE, Message: &pb.Message{Id: req.MessageId, TopicId: req.TopicId, UserId: req.UserId, Text: "", CreatedAt: timestamppb.New(time.Unix(-69420, 0)), Likes: -1}, EventAt: timestamppb.Now()})
	return &emptypb.Empty{}, nil

}

func (s *server) LikeMessage(ctx context.Context, req *pb.LikeMessageRequest) (*pb.Message, error) {

	if len(s.masters) != 0 {
		return nil, status.Error(codes.Internal, "write zahteva poslala na ne-head node v verigi")
	}
	// authenticate user
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	// transaction to keep things consistent // ????
	tx := s.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	like := &MessageLike{
		MessageID: req.MessageId,
		UserID:    userID,
	}

	if err := tx.Create(like).Error; err != nil {
		tx.Rollback()
		return nil, status.Error(
			codes.AlreadyExists,
			"message already liked by this user",
		)
	}

	if err := tx.Model(&Message{}).
		Where("id = ? AND topic_id = ?", req.MessageId, req.TopicId).
		UpdateColumn("likes", gorm.Expr("likes + 1")).
		Error; err != nil {

		tx.Rollback()
		return nil, status.Error(codes.Internal, err.Error())
	}

	var msg Message
	if err := tx.First(&msg, req.MessageId).Error; err != nil {
		tx.Rollback()
		return nil, status.Error(codes.Internal, err.Error())
	}

	if err := tx.Commit().Error; err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	pbmessage := &pb.Message{
		Id:        msg.ID,
		TopicId:   msg.TopicID,
		UserId:    msg.UserID,
		Text:      msg.Text,
		Likes:     msg.Likes,
		CreatedAt: timestamppb.New(msg.CreatedAt),
	}

	s.NotifySlavesLike(req)
	s.NotifySlavesMessage(pbmessage)

	return pbmessage, nil
}

func generateSubscriptionToken(userID int64, topicIDs []int64) (string, error) {
	claims := SubscriptionClaims{
		UserID:   userID,
		TopicIDs: topicIDs,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}
func (s *server) chooseSubscriptionNode(userID int64, topicIDs []int64) (*pb.NodeInfo, error) {
	s.cpc_lock.RLock()
	if s.cpc == nil {
		defer s.cpc_lock.RUnlock()
		return &pb.NodeInfo{NodeId: s.url, Address: s.url}, nil
	}
	ctx, _ := context.WithDeadline(s.cpc.ctx(), time.Now().Add(time.Second))
	res, err := s.cpc.api.GetRandomNode(ctx, &emptypb.Empty{}) // FIXME s.cpc je lockan med network requestom!!!
	s.cpc_lock.RUnlock()
	if err != nil {
		return nil, err
	}
	return res, nil
}
func (s *server) GetSubscriptionNode(ctx context.Context, req *pb.SubscriptionNodeRequest) (*pb.SubscriptionNodeResponse, error) {
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	var count int64
	if err := s.db.WithContext(ctx).
		Model(&Topic{}).
		Where("id IN ?", req.TopicId).
		Count(&count).
		Error; err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if count != int64(len(req.TopicId)) {
		return nil, status.Error(
			codes.NotFound,
			"one or more topics do not exist",
		)
	}
	node, err := s.chooseSubscriptionNode(userID, req.TopicId)
	if err != nil {
		return nil, err
	}
	subToken, err := generateSubscriptionToken(userID, req.TopicId)
	if err != nil {
		return nil, status.Error(
			codes.Internal,
			"failed to generate subscription token",
		)
	}
	return &pb.SubscriptionNodeResponse{
		SubscribeToken: subToken,
		Node:           node,
	}, nil
}

func (s *server) ListTopics(ctx context.Context, req *emptypb.Empty) (*pb.ListTopicsResponse, error) {
	var topics []Topic

	// Query all topics from the DB
	if err := s.db.WithContext(ctx).Find(&topics).Error; err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	allTopics := make([]*pb.Topic, 0, len(topics))
	for _, t := range topics {
		allTopics = append(allTopics, &pb.Topic{
			Id:   t.ID,
			Name: t.Name,
		})
	}

	return &pb.ListTopicsResponse{
		Topics: allTopics,
	}, nil
}

func (s *server) GetUsers(ctx context.Context, req *emptypb.Empty) (*pb.GetUsersResponse, error) {
	var users []User

	if err := s.db.WithContext(ctx).Find(&users).Error; err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	allUsers := make([]*pb.User, 0, len(users))
	for _, t := range users {
		allUsers = append(allUsers, &pb.User{
			Id:   t.ID,
			Name: t.Name,
		})
	}

	return &pb.GetUsersResponse{
		Users: allUsers,
	}, nil
}
func (s *server) GetLikes(ctx context.Context, req *emptypb.Empty) (*pb.GetLikesResponse, error) {
	var likes []MessageLike

	if err := s.db.WithContext(ctx).Find(&likes).Error; err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	allLikes := make([]*pb.LikeMessageRequest, 0, len(likes))
	for _, l := range likes {
		allLikes = append(allLikes, &pb.LikeMessageRequest{
			TopicId:   -1, // FIXME get topicid for message
			MessageId: l.MessageID,
			UserId:    l.UserID,
		})
	}

	return &pb.GetLikesResponse{
		Likes: allLikes,
	}, nil
}

// to do: reindeksiranje messageov. V vsakem topicu posebej začnemo sporočila številčiti z 1
// (zato tudi pri vsakem update/delete/post message zraven podamo topic ID!!)
// lahko namesto rewritanja tega samo pri getMessages gledaš najmanjših k indeksov ...
// najbrž ni tak namen ...

func (s *server) GetMessages(ctx context.Context, req *pb.GetMessagesRequest) (*pb.GetMessagesResponse, error) {

	var messages []Message

	topicclause := "topic_id = ?"
	if req.TopicId == -1 {
		topicclause = "(topic_id = ? OR 1 = 1)"
	}

	err := s.db.WithContext(ctx).
		Where(topicclause, req.TopicId).
		Where(
			"( ? = 0 OR id > ? )", // ker ima vsak message qunique ID ne glede na topic ...
			req.FromMessageId,
			req.FromMessageId,
		).
		Order("id ASC").
		Limit(int(req.Limit)).
		Find(&messages).
		Error

	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	allMessages := make([]*pb.Message, 0, len(messages))
	for _, m := range messages {
		allMessages = append(allMessages, &pb.Message{
			Id:        m.ID,
			TopicId:   m.TopicID,
			UserId:    m.UserID,
			Text:      m.Text,
			Likes:     m.Likes,
			CreatedAt: timestamppb.New(m.CreatedAt),
		})
	}

	return &pb.GetMessagesResponse{
		Messages: allMessages,
	}, nil
}
func (s *server) obvesti_naročnike(dogodek pb.MessageEvent) {
	s.naročniki_lock.Lock()
	defer s.naročniki_lock.Unlock()
	naročniki_kopija := make(map[chan pb.MessageEvent]pb.SubscribeTopicRequest)
	for k, v := range s.naročniki {
		naročniki_kopija[k] = v
	}
	log.Printf("OBVESTI NAROČNIKE %v", dogodek)
	for nchan, ninfo := range naročniki_kopija {
		log.Printf("NAROČNIK %v %v", ninfo, nchan)
		if !slices.Contains(ninfo.TopicId, dogodek.Message.TopicId) && len(ninfo.TopicId) == 0 || ninfo.FromMessageId < dogodek.Message.Id {
			// continue
		}
		select {
		case nchan <- dogodek:
		default:
			log.Println("odklapljam naročnika, ker ima poln medpomnilnik")
			delete(s.naročniki, nchan)
			close(nchan)
		}
	}
}
func (s *server) SubscribeTopic(req *pb.SubscribeTopicRequest, stream grpc.ServerStreamingServer[pb.MessageEvent]) error {
	s.naročniki_lock.Lock()
	naročnina := make(chan pb.MessageEvent, 16)
	s.naročniki[naročnina] = *req
	s.naročniki_lock.Unlock()
	defer func() {
		s.naročniki_lock.Lock()
		delete(s.naročniki, naročnina)
		s.naročniki_lock.Unlock()
		close(naročnina)
	}()
	for event := range naročnina {
		log.Printf("POŠILJAM DOGODEK NAROČNIKU %v", event)
		if err := stream.Send(&event); err != nil {
			return err
		}
	}
	return nil
}
