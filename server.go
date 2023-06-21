// SPDX-License-Identifier: MIT
//
// Copyright 2021 Andrew Bursavich. All rights reserved.
// Use of this source code is governed by The MIT License
// which can be found in the LICENSE file.

// Package graceful runs processes with graceful shutdown.
package graceful

import (
	"context"
	"net"
	"net/http"
	"sync"
)

// HTTPServerProcess converts an http.Server and net.Listener into a graceful.Process.
func HTTPServerProcess(srv *http.Server, lis net.Listener) Process {
	return &httpServer{srv, lis}
}

type httpServer struct {
	srv *http.Server
	lis net.Listener
}

func (s *httpServer) Run(ctx context.Context) error {
	if err := s.srv.Serve(s.lis); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *httpServer) Shutdown(ctx context.Context) error {
	s.srv.SetKeepAlivesEnabled(false)
	return s.srv.Shutdown(ctx)
}

// A GRPCServer is an interface for a grpc.Server.
type GRPCServer interface {
	Serve(net.Listener) error
	GracefulStop()
	Stop()
}

type grpcServer struct {
	srv GRPCServer
	lis net.Listener

	once  sync.Once
	errCh chan error
	done  chan struct{}
	err   error
}

// GRPCServerProcess converts a grpc.Server and its net.Listener into a graceful.Process.
func GRPCServerProcess(srv GRPCServer, lis net.Listener) Process {
	return &grpcServer{
		srv:   srv,
		lis:   lis,
		errCh: make(chan error),
		done:  make(chan struct{}),
	}
}

func (s *grpcServer) Run(ctx context.Context) error {
	return s.srv.Serve(s.lis)
}

func (s *grpcServer) Shutdown(ctx context.Context) error {
	s.once.Do(func() { go s.shutdown() })
	select {
	case <-s.done:
		return s.err
	case <-ctx.Done():
	}
	select {
	case <-s.done:
		return s.err
	case s.errCh <- ctx.Err():
	}
	<-s.done
	return s.err
}

func (s *grpcServer) shutdown() {
	defer close(s.done)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.srv.GracefulStop()
	}()
	select {
	case <-done:
		return
	case err := <-s.errCh:
		s.err = err
		s.srv.Stop()
		<-done
	}
}
