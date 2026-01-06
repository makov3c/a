// Komunikacija po protokolu gRPC
// strežnik

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	pb "4a.si/razpravljalnica/grpc/protobufRazpravljalnica"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/golang-jwt/jwt/v5"
)

// TODO: move to env variable
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
	ID        int64 `gorm:"primaryKey"`
	MessageID int64 `gorm:"not null;index"`
	UserID    int64 `gorm:"not null;index"`

	_ struct{} `gorm:"uniqueIndex:idx_message_user"` // (user can like a message only once)
}

type SubscriptionNode struct {
	ID      string
	Address string
}

var subscriptionNodes = []SubscriptionNode{
	{ID: "node-1", Address: "localhost:9876"},
}

type server struct {
	pb.UnimplementedMessageBoardServer

	db *gorm.DB
	// lastUserID   int64
	// mu_userCount sync.Mutex
}

func Server(url string) {

	// database z uporabniki
	db, err := gorm.Open(sqlite.Open("baza.db"), &gorm.Config{})
	if err != nil {
		panic("failed to connect to the database")
	}

	db.Exec("PRAGMA foreign_keys = ON") // ker imava sqlite

	if err := db.AutoMigrate(&User{}, &Topic{}, &Message{}, &MessageLike{}); err != nil {
		panic("failed to migrate database")
	}

	// pripravimo strežnik gRPC
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(authInterceptor),
	)

	s := &server{db: db} // creates a new value of type server, assigns field db to the vasiable users_db (a *gorm DB)
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
	fmt.Printf("gRPC server listening at %v%v\n", hostName, url)
	// začnemo s streženjem
	if err := grpcServer.Serve(listener); err != nil {
		panic(err)
	}
}

func (s *server) CreateUser(ctx context.Context, req *pb.CreateUserRequest) (*pb.User, error) {

	user := &User{Name: req.Name}
	// WithContext - if client cancels the request the db operation is canceled too
	if err := s.db.WithContext(ctx).Create(user).Error; err != nil {
		return nil, err
	}

	return &pb.User{Id: user.ID, Name: user.Name}, nil
}

func (s *server) CreateTopic(ctx context.Context, req *pb.CreateTopicRequest) (*pb.Topic, error) {

	topic := &Topic{Name: req.Name}

	if err := s.db.WithContext(ctx).Create(topic).Error; err != nil {
		return nil, err
	}

	return &pb.Topic{Id: topic.ID, Name: topic.Name}, nil

}

// --------------- ????
// TODO: napiši teste(?) za slabe stvari (ko avtentikacija ne dela oz. uspešno dela in zavrne)
func generateJWT(userID int64) (string, error) {
	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
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

func authInterceptor(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {

	switch info.FullMethod {
	case
		"/razpravljalnica.MessageBoard/CreateUser",
		"/razpravljalnica.MessageBoard/Login":

		fmt.Println(">>> allow-listed method, skipping auth")
		return handler(ctx, req)
	}

	// everything below REQUIRES authentication
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}

	authHeader := md.Get("authorization")
	if len(authHeader) == 0 {
		fmt.Println(">>> authInterceptor called for:", info.FullMethod)
		return nil, status.Error(codes.Unauthenticated, "missing authorization header")
	}

	parts := strings.SplitN(authHeader[0], " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return nil, status.Error(codes.Unauthenticated, "invalid authorization format")
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

// ----------- ???

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

	return &pb.LoginResponse{
		Token: token,
	}, nil
}

func (s *server) PostMessage(ctx context.Context, req *pb.PostMessageRequest) (*pb.Message, error) {

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

	return &pb.Message{
		Id:        message.ID,
		TopicId:   message.TopicID,
		UserId:    message.UserID,
		Text:      message.Text,
		Likes:     message.Likes,
		CreatedAt: timestamppb.New(message.CreatedAt),
	}, nil

}

func (s *server) UpdateMessage(ctx context.Context, req *pb.UpdateMessageRequest) (*pb.Message, error) {

	// authenticate user
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

	return &pb.Message{
		Id:        msg.ID,
		TopicId:   msg.TopicID,
		UserId:    msg.UserID,
		Text:      msg.Text,
		Likes:     msg.Likes,
		CreatedAt: timestamppb.New(msg.CreatedAt),
	}, nil
}

func (s *server) DeleteMessage(ctx context.Context, req *pb.DeleteMessageRequest) (*emptypb.Empty, error) {

	// authenticate user
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	result := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", req.MessageId, userID).
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

	return &emptypb.Empty{}, nil

}

func (s *server) LikeMessage(ctx context.Context, req *pb.LikeMessageRequest) (*pb.Message, error) {

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

	return &pb.Message{
		Id:        msg.ID,
		TopicId:   msg.TopicID,
		UserId:    msg.UserID,
		Text:      msg.Text,
		Likes:     msg.Likes,
		CreatedAt: timestamppb.New(msg.CreatedAt),
	}, nil
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

func chooseSubscriptionNode(userID int64, topicIDs []int64) SubscriptionNode {

	return subscriptionNodes[0]
}

func (s *server) GetSubscriptionNode(ctx context.Context, req *pb.SubscriptionNodeRequest) (*pb.SubscriptionNodeResponse, error) {

	// authenticate user
	userID, err := userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if len(req.TopicId) == 0 {
		return nil, status.Error(
			codes.InvalidArgument,
			"no topics provided",
		)
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

	// choose the subscription node
	chosen := chooseSubscriptionNode(userID, req.TopicId)

	node := &pb.NodeInfo{
		NodeId:  chosen.ID,
		Address: chosen.Address,
	}

	// issue a subscription token
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
