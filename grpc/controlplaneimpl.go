package main
import (
	"os"
	"net"
	"log"
	"context"
	"strings"
	"math/rand"
	"sync"
	"time"
	pb "4a.si/razpravljalnica/grpc/protobufRazpravljalnica"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)
type strežnik struct {
	master string // url, "" if no master
	slave string // url, "" if no slave
}
type controlplaneserver struct {
	pb.UnimplementedControlPlaneServer
	strežniki map[string]*strežnik
	head string // url of head node, you can then traverse the veriga
	s2s_psk string
	bind string
	veriga_lock sync.RWMutex
}
func ControlPlaneServer (bind string, s2s_psk string) error {
	s := &controlplaneserver{bind: bind, s2s_psk: s2s_psk, strežniki: make(map[string]*strežnik)}
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(authInterceptor(s)),
	)
	pb.RegisterControlPlaneServer(grpcServer, s)
	hostName, err := os.Hostname()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", bind)
	if err != nil {
		return err
	}
	log.Printf("controlplane server listening at hostname %v, bind url %v", hostName, bind)
	var wg sync.WaitGroup
	wg.Add(1)
	go func () {
		if err := grpcServer.Serve(listener); err != nil {
			panic(err)
		}
		wg.Done()
	}()
	for {
		time.Sleep(time.Second)
		veriga := s.veriga(false)
		log.Printf("healthcheck: veriga je %v", veriga)
		for index, url := range veriga {
			c, err := NewClient(url)
			if err != nil {
				log.Printf("healthcheck: NewClient %v failed with %v", url, err)
				goto ERR
			}
			c.token = s.s2s_psk
			_, err = c.api.Ping(c.ctx(), &emptypb.Empty{})
			if err != nil {
				log.Printf("healthcheck: Ping %v failed with %v", url, err)
				goto ERR
			}
			continue
			ERR:
			s.veriga_lock.Lock()
			if index != 0 {
				c, err := NewClient(veriga[index-1])
				if err != nil {
					panic(err)
				}
				c.token = s.s2s_psk
				newslaves := []string{}
				if index+1 < len(veriga) {
					newslaves = []string{veriga[index+1]}
				}
				_, err = c.api.SetSlaves(c.ctx(), &pb.SetSlavesRequest{Slaves: newslaves})
				if err != nil {
					log.Printf("idk, pizdarija SetSlaves: %v", err) // TODO
				}
				newmasters := []string{}
				if index+1 < len(veriga) {
					newmasters = []string{veriga[index-1]}
				}
				if index+1 < len(veriga) {
					c2, err := NewClient(veriga[index+1])
					if err != nil {
						panic(err)
					}
					c2.token = s.s2s_psk
					_, err = c2.api.FetchFrom(c2.ctx(), &pb.FetchFromRequest{Masters: newmasters})
					if err != nil {
						log.Printf("idk, pizdarija FetchFrom: %v", err) // TODO
					}
					s.strežniki[veriga[index+1]].master = veriga[index-1]
					s.strežniki[veriga[index-1]].slave = veriga[index+1]
				} else {
					s.strežniki[veriga[index-1]].slave = ""
				}
			} else { // index == 0
				s.head = ""
				if index+1 < len(veriga) {
					c2, err := NewClient(veriga[index+1])
					if err != nil {
						panic(err)
					}
					c2.token = s.s2s_psk
					_, err = c2.api.FetchFrom(c2.ctx(), &pb.FetchFromRequest{Masters: []string{}})
					if err != nil {
						log.Printf("#idk, pizdarija FetchFrom: %v", err) // TODO
					}
					s.head = veriga[index+1]
					s.strežniki[veriga[index+1]].master = ""
				}
			}
			delete(s.strežniki, url)
			s.veriga_lock.Unlock()
			break
		}
	}
	wg.Wait()
	return nil // TODO naredi periodičen healthcheck strežnikov v ravnini in pomeči ven tiste, ki so pokvarjeni
}
func authInterceptor (s *controlplaneserver) grpc.UnaryServerInterceptor {
	return func (ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if info.FullMethod == "/razpravljalnica.ControlPlane/GetClusterState" {
			return handler(ctx, req)
		}
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}
		authHeader := md.Get("authorization")
		if len(authHeader) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing authorization header in controlplaneimpl.go")
		}
		parts := strings.SplitN(authHeader[0], " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			return nil, status.Error(codes.Unauthenticated, "invalid authorization format")
		}
		if parts[1] != s.s2s_psk {
			return nil, status.Error(codes.Unauthenticated, "invalid s2s psk")
		}
		return handler(ctx, req)
	}
}
func (s *controlplaneserver) veriga (already_locked bool) []string {
	if !already_locked {
		s.veriga_lock.RLock()
		defer s.veriga_lock.RUnlock()
	}
	v := []string{}
	r := s.head
	_, ok := s.strežniki[r]
	if !ok {
		return v
	}
	v = append(v, r)
	for {
		if s.strežniki[r].slave == "" {
			return v
		}
		_, ok := s.strežniki[s.strežniki[r].slave]
		if !ok {
			return []string{}
		}
		r = s.strežniki[r].slave
		v = append(v, r)
	}
	return v
}
func (s *controlplaneserver) tail (already_locked bool) string {
	if !already_locked {
		s.veriga_lock.RLock()
		defer s.veriga_lock.RUnlock()
	}
	v := s.veriga(true)
	if len(v) == 0 {
		return ""
	}
	return v[len(v)-1]
}
func (s *controlplaneserver) GetClusterState (ctx context.Context, req *emptypb.Empty) (*pb.GetClusterStateResponse, error) {
	return &pb.GetClusterStateResponse{Head: &pb.NodeInfo{NodeId: s.head, Address: s.head}, Tail: &pb.NodeInfo{NodeId: s.tail(false), Address: s.tail(false)}}, nil
}
func (s *controlplaneserver) GetRandomNode (ctx context.Context, req *emptypb.Empty) (*pb.NodeInfo, error) {
	s.veriga_lock.RLock()
	defer s.veriga_lock.RUnlock()
	v := s.veriga(true)
	url := v[rand.Intn(len(v))]
	return &pb.NodeInfo{NodeId: url, Address: url}, nil
}
func (s *controlplaneserver) ServerAvailable (ctx context.Context, req *pb.NodeInfo) (*emptypb.Empty, error) {
	log.Printf("ServerAvailable called ", req, "\n")
	c, err := NewClient(req.Address)
	if err != nil {
		log.Printf("ServerAvailable: NewClient req.Address ", req.Address, " failed: ", err, "\n")
		return nil, err
	}
	c.token = s.s2s_psk
	_, err = c.api.Ping(c.ctx(), &emptypb.Empty{})
	if err != nil {
		msg := "req.Address " + req.Address + " se NE odziva na ping"
		log.Printf(msg)
		return nil, status.Error(codes.Internal, msg)
	} else {
		log.Printf("req.Address ", req.Address, " se odziva na ping")
	}
	s.veriga_lock.RLock()
	tail := s.tail(true)
	s.veriga_lock.RUnlock()
	if tail != "" {
		ctail, err := NewClient(tail)
		if err != nil {
			log.Printf("ServerAvailable: NewClient tail ", tail, " failed: ", err, "\n")
			return nil, err
		}
		ctail.token = s.s2s_psk
		_, err = ctail.api.SetSlaves(ctail.ctx(), &pb.SetSlavesRequest{Slaves: []string{req.Address}})
		log.Printf("SetSlaves from tail " + tail + " returned")
		if err != nil {
			log.Printf("SetSlaves on ", tail, " je failal z ", err, "s.")
			return nil, err
		}
		log.Printf("ServerAvailable: attempting to call FetchFrom on ", req.Address)
		_, err = c.api.FetchFrom(c.ctx(), &pb.FetchFromRequest{Masters: []string{tail}})
		if err != nil {
			log.Printf("FetchFrom na req.Address=", req.Address, " je failal: ", err)
			return nil, err
		}
	}

	s.veriga_lock.Lock()
	if tail != s.tail(true) {
		log.Printf("tail != s.tail(true)")
		s.veriga_lock.Unlock()
		return nil, status.Error(codes.Internal, "med izvajanjem FetchFrom se je zamenjal tail")
	}
	if tail == "" {
		s.head = req.Address
		s.strežniki[req.Address] = &strežnik{master: "", slave: ""}
	} else {
		s.strežniki[tail].slave = req.Address
		s.strežniki[req.Address] = &strežnik{master: tail, slave: ""}
	}
	log.Printf("nova veriga je ", s.veriga(true))
	s.veriga_lock.Unlock()
	return &emptypb.Empty{}, nil
}
