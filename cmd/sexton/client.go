package main

import (
	"io"

	sextonv1 "github.com/michaelquigley/sexton/api/v1"
	"github.com/michaelquigley/sexton/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type agentConn interface {
	io.Closer
}

var dialAgentFn = dialAgent

func dialAgent() (sextonv1.SextonClient, agentConn, error) {
	target := "unix://" + config.SocketPath()
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}
	return sextonv1.NewSextonClient(conn), conn, nil
}
