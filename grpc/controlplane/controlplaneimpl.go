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
	"io"
	"path/filepath"
	"slices"
	pb "4a.si/razpravljalnica/grpc/protobufRazpravljalnica"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb"
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
	raft *raft.Raft
	fsm *FSM // implements raft.FSM
	raft_dir string // idealno dovoli da to lahko uporabnik nastavi na argv
	raft_bind string
	raft_address string
	bootstrap bool
	myurl string
}
type FSM struct{} // raft uporabljamo samo za izbiro leaderja, state machine je prazen
func (f *FSM) Apply (l *raft.Log) interface{} {
	return nil
}
func (f *FSM) Snapshot () (raft.FSMSnapshot, error) {
	return &FSMSnapshot{}, nil
}
func (f *FSM) Restore (io.ReadCloser) error {
	return nil
}
type FSMSnapshot struct{}
func (s *FSMSnapshot) Persist (sink raft.SnapshotSink) error {
	_ = sink.Close()
	return nil
}
func (s *FSMSnapshot) Release () {}
func (s *controlplaneserver) setup_raft () error {
	if s.raft_dir == "" {
		rd, err := os.MkdirTemp("", "raft-"+s.raft_address)
		if err != nil {
			panic(err)
		}
		s.raft_dir = rd
	}
	os.MkdirAll(s.raft_dir, 0750)
	config := raft.DefaultConfig()
	config.LocalID = raft.ServerID(s.raft_address + " " + s.myurl)
	config.SnapshotInterval = 30 * time.Second
	config.SnapshotThreshold = 2
	logStore, err := raftboltdb.NewBoltStore(filepath.Join(s.raft_dir, "raft.db"))
	if err != nil {
		return err
	}
	stableStore := logStore
	snapshotStore, err := raft.NewFileSnapshotStore(s.raft_dir, 2, os.Stderr)
	if err != nil {
		return err
	}
	addr, err := net.ResolveTCPAddr("tcp", s.raft_address)
	if err != nil {
		return err
	}
	transport, err := raft.NewTCPTransport(s.raft_bind, addr, 3, 10*time.Second, os.Stderr)
	if err != nil {
		return err
	}
	s.fsm = &FSM{}
	r, err := raft.NewRaft(config, s.fsm, logStore, stableStore, snapshotStore, transport)
	if err != nil {
		return err
	}
	s.raft = r
	if r.State() == raft.Follower && s.bootstrap {
		log.Printf("setup_raft: bootstrap mode active, raft id is %v", config.LocalID)
		configuration := raft.Configuration{
			Servers: []raft.Server{
				{
					ID: config.LocalID,
					Address: raft.ServerAddress(s.raft_address),
				},
			},
		}
		r.BootstrapCluster(configuration)
	}
	return nil
}
func ControlPlaneServer (bind string, s2s_psk string, raftbind string, raftaddress string, bootstrap bool, cluster string, myurl string) error {
	s := &controlplaneserver{bind: bind, s2s_psk: s2s_psk, strežniki: make(map[string]*strežnik), raft_bind: raftbind, raft_address: raftaddress, bootstrap: bootstrap, myurl: myurl}
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
	if raftbind != "" {
		err := s.setup_raft()
		if err != nil {
			return err
		}
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func () {
		if err := grpcServer.Serve(listener); err != nil {
			panic(err)
		}
		wg.Done()
	}()
	if cluster != "" {
		cpc, err := NewControlPlaneClient(cluster, s.s2s_psk)
		if err != nil {
			panic(err)
		}
		res, err := cpc.GetRaftLeader(time.Now().Add(time.Second))
		if err != nil {
			panic(err)
		}
		log.Printf("Got address of raft leader. It is %v", res)
		cpc.Close()
		cpc, err = NewControlPlaneClient(res.ControlplaneAddress, s.s2s_psk)
		if err != nil {
			panic(err)
		}
		pbcpa := &pb.ControlPlaneInfo{ControlplaneAddress: myurl, RaftAddress: s.raft_address}
		log.Printf("Calling ControlPlaneAvailable with %v", pbcpa)
		_, err = cpc.api.ControlPlaneAvailable(cpc.ctx(), pbcpa)
		if err != nil {
			panic(err)
		}
	}
	for {
		time.Sleep(time.Second)
		if s.raft != nil {
			if s.raft.State() != raft.Leader {
				log.Printf("I am not raft leader %v", s.raft.Stats())
				continue
			} else {
				log.Printf("I am raft leader. Raft state: %v", s.raft.Stats())
			}
		}
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
					log.Printf("idk, pizdarija SetSlaves: %v", err) // FIXME
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
						log.Printf("idk, pizdarija FetchFrom: %v", err) // FIXME
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
						log.Printf("#idk, pizdarija FetchFrom: %v", err) // FIXME
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
	return nil
}
func authInterceptor (s *controlplaneserver) grpc.UnaryServerInterceptor {
	return func (ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if slices.Contains([]string{"/razpravljalnica.ControlPlane/GetClusterState", "/razpravljalnica.ControlPlane/GetRaftLeader", "/razpravljalnica.ControlPlane/GetControlPlanes"}, info.FullMethod) {
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
func (s *controlplaneserver) Ping (ctx context.Context, req *emptypb.Empty) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}
func (s *controlplaneserver) GetControlPlanes (ctx context.Context, req *emptypb.Empty) (*pb.ControlPlanes, error) {
	if s.raft == nil {
		return nil, status.Error(codes.FailedPrecondition, "ControlPlanes: raft is not enabled on this controlplane")
	}
	cpinfos := []*pb.ControlPlaneInfo{}
	future := s.raft.GetConfiguration()
	err := future.Error()
	if err != nil {
		return nil, err
	}
	for _, srv := range future.Configuration().Servers {
		log.Printf("GetControlPlanes: Server %v", srv)
		parts := strings.SplitN(string(srv.ID), " ", 2)
		cpinfos = append(cpinfos, &pb.ControlPlaneInfo{RaftAddress: parts[0], ControlplaneAddress: parts[1]})
	}
	log.Printf("GetControlPlanes: cpinfos je %v", cpinfos)
	return &pb.ControlPlanes{ControlPlanes: cpinfos}, nil
}
func (s *controlplaneserver) GetClusterState (ctx context.Context, req *emptypb.Empty) (*pb.GetClusterStateResponse, error) {
	if s.raft != nil && s.raft.State() != raft.Leader {
		return nil, status.Error(codes.Internal, "GetClusterState: I am not the raft leader.")
	}
	return &pb.GetClusterStateResponse{Head: &pb.NodeInfo{NodeId: s.head, Address: s.head}, Tail: &pb.NodeInfo{NodeId: s.tail(false), Address: s.tail(false)}}, nil
}
func (s *controlplaneserver) GetRaftLeader (ctx context.Context, req *emptypb.Empty) (*pb.ControlPlaneInfo, error) {
	if s.raft == nil {
		return nil, status.Error(codes.FailedPrecondition, "GetRaftLeader: raft is not enabled on this controlplane")
	}
	_, serverid := s.raft.LeaderWithID()
	if serverid == "" {
		return nil, status.Error(codes.Unavailable, "No leader elected currently, sorry. Please try again.")
	}
	parts := strings.SplitN(string(serverid), " ", 2)
	return &pb.ControlPlaneInfo{RaftAddress: parts[0], ControlplaneAddress: parts[1]}, nil
}
func (s *controlplaneserver) ControlPlaneAvailable (ctx context.Context, req *pb.ControlPlaneInfo) (*emptypb.Empty, error) {
	if s.raft == nil {
		return nil, status.Error(codes.FailedPrecondition, "ControlPlaneAvailable: raft is not enabled on this controlplane")
	}
	if s.raft.State() != raft.Leader {
		return nil, status.Error(codes.Internal, "ControlPlaneAvailable: this server is not the raft leader, call GetRaftLeader to get the leader")
	}
	c, err := NewControlPlaneClient(req.ControlplaneAddress, s.s2s_psk)
	if err != nil {
		return nil, err
	}
	_, err = c.api.Ping(c.ctx(), &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	log.Printf("Adding new voter %v", req)
	future := s.raft.AddVoter(raft.ServerID(req.RaftAddress + " " + req.ControlplaneAddress), raft.ServerAddress(req.RaftAddress), 0, 0)
	err = future.Error() // blocks
	if err != nil {
		return nil, err
	}
	log.Printf("successfully added voter with index %d", future.Index())
	return &emptypb.Empty{}, nil
}
func (s *controlplaneserver) GetRandomNode (ctx context.Context, req *emptypb.Empty) (*pb.NodeInfo, error) {
	if s.raft != nil && s.raft.State() != raft.Leader {
		return nil, status.Error(codes.Internal, "GetClusterState: I am not the raft leader.")
	}
	s.veriga_lock.RLock()
	defer s.veriga_lock.RUnlock()
	v := s.veriga(true)
	url := v[rand.Intn(len(v))]
	return &pb.NodeInfo{NodeId: url, Address: url}, nil
}
func (s *controlplaneserver) ServerAvailable (ctx context.Context, req *pb.NodeInfo) (*emptypb.Empty, error) {
	if s.raft != nil && s.raft.State() != raft.Leader {
		return nil, status.Error(codes.Internal, "GetClusterState: I am not the raft leader.")
	}
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
		log.Printf("req.Address %v se odziva na ping", req.Address)
	}
	s.veriga_lock.RLock()
	veriga := s.veriga(true)
	curslaves := []string{}
	curmasters := []string{}
	vverigi := false
	if slices.Contains(veriga, req.Address) {
		if s.strežniki[req.Address].slave != "" {
			curslaves = []string{s.strežniki[req.Address].slave}
		}
		if s.strežniki[req.Address].master != "" {
			curmasters = []string{s.strežniki[req.Address].master}
		}
		vverigi = true
	}
	s.veriga_lock.RUnlock()
	var tail string
	if vverigi {
		goto SENDSETSLAVES
	}
	tail = s.tail(false)
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
	s.veriga_lock.Unlock()
	SENDSETSLAVES:
	if vverigi {
		log.Printf("ServerAvailable vverigi: attempting to call FetchFrom on ", req.Address)
		_, err = c.api.FetchFrom(c.ctx(), &pb.FetchFromRequest{Masters: curmasters})
		if err != nil {
			log.Printf("FetchFrom vverigi na req.Address=", req.Address, " je failal: ", err)
			return nil, err
		}
		_, err = c.api.SetSlaves(c.ctx(), &pb.SetSlavesRequest{Slaves: curslaves})
		if err != nil {
			log.Printf("idk, pizdarija SetSlaves: %v", err) // FIXME
		}
	}
	log.Printf("nova veriga je ", s.veriga(true))
	return &emptypb.Empty{}, nil
}
