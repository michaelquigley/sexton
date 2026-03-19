package rpc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/michaelquigley/df/dl"
	sextonv1 "github.com/michaelquigley/sexton/api/v1"
	"google.golang.org/grpc"
)

// Server manages the gRPC server lifecycle over a Unix domain socket.
type Server struct {
	socketPath string
	ctrl       AgentController
	gs         *grpc.Server
	lis        net.Listener
}

func NewServer(socketPath string, ctrl AgentController) *Server {
	return &Server{
		socketPath: socketPath,
		ctrl:       ctrl,
	}
}

// Start creates the Unix socket and begins serving gRPC requests in a background goroutine.
func (s *Server) Start() error {
	if err := s.checkStaleSocket(); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0700); err != nil {
		return fmt.Errorf("failed to create socket directory: %w", err)
	}

	lis, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on '%s': %w", s.socketPath, err)
	}
	s.lis = lis

	s.gs = grpc.NewServer()
	sextonv1.RegisterSextonServer(s.gs, &handler{ctrl: s.ctrl})

	go func() {
		if err := s.gs.Serve(lis); err != nil {
			dl.Errorf("grpc server error: %v", err)
		}
	}()

	dl.Infof("control plane listening on '%s'", s.socketPath)
	return nil
}

// Stop gracefully stops the gRPC server and removes the socket file.
func (s *Server) Stop() {
	if s.gs != nil {
		s.gs.GracefulStop()
	}
	_ = os.Remove(s.socketPath)
	dl.Infof("control plane stopped")
}

// checkStaleSocket determines if the socket file already exists. if it does,
// it tries to connect to detect whether another agent is running.
func (s *Server) checkStaleSocket() error {
	_, err := os.Stat(s.socketPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	// socket file exists — try to connect to see if it's active
	conn, err := net.Dial("unix", s.socketPath)
	if err == nil {
		// connection succeeded — another agent is running
		_ = conn.Close()
		return fmt.Errorf("another agent is already running (socket '%s' is active)", s.socketPath)
	}

	// connection failed — stale socket, remove it
	dl.Infof("removing stale socket '%s'", s.socketPath)
	return os.Remove(s.socketPath)
}
