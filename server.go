// SPDX-License-Identifier: MIT
//
// Copyright 2021 Andrew Bursavich. All rights reserved.
// Use of this source code is governed by The MIT License
// which can be found in the LICENSE file.

// Package graceful provides graceful shutdown for servers.
package graceful

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
)

// A Server is a networked server.
type Server interface {
	// Serve serves connections accepted from the listener. It does not return
	// an error if the listener is closed by the GracefulShutown method.
	Serve(net.Listener) error

	// GracefulShutdown immediately closes the server's listener and signals to
	// clients as necessary that it is going away. It waits for pending requests
	// to finish or until the context is closed. If all pending requests finish,
	// no error is returned.
	GracefulShutdown(context.Context) error
}

// FromHTTP converts an http.Server into a graceful.Server.
func FromHTTP(srv *http.Server) Server {
	return (*httpServer)(srv)
}

type httpServer http.Server

func (s *httpServer) Serve(lis net.Listener) error {
	srv := (*http.Server)(s)
	if err := srv.Serve(lis); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *httpServer) GracefulShutdown(ctx context.Context) error {
	srv := (*http.Server)(s)
	srv.SetKeepAlivesEnabled(false)
	return srv.Shutdown(ctx)
}

// ServerConfig specifies a server with graceful shutdown parameters.
type ServerConfig struct {
	_ struct{}

	// Server is the networked server.
	Server Server

	// ShutdownDelay gives time for load balancers to remove the server from
	// their backend pools after a shutdown signal is received and before it
	// stops listening.
	ShutdownDelay time.Duration

	// ShutdownGrace gives time for pending requests to complete before the
	// server forcibly shuts down.
	ShutdownGrace time.Duration

	// Logger optionally adds the ability to log messages, both errors and not.
	Logger logr.Logger

	initOnce   sync.Once
	warnCtx    context.Context
	warnCancel context.CancelFunc
}

func (cfg *ServerConfig) init() {
	cfg.initOnce.Do(func() {
		cfg.Logger = loggerOrDiscard(cfg.Logger)
		cfg.warnCtx, cfg.warnCancel = context.WithCancel(context.TODO())
	})
}

// ShuttingDown returns a channel that is closed when the server encounters
// an error or it receives its first signal to begin shutting down.
func (cfg *ServerConfig) ShuttingDown() <-chan struct{} {
	cfg.init()
	return cfg.warnCtx.Done()
}

// ListenAndServe listens on the given address and calls Serve.
func (cfg *ServerConfig) ListenAndServe(ctx context.Context, addr string) error {
	cfg.init()
	defer cfg.warnCancel()

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		cfg.Logger.Error(err, "Server failed to listen", "address", addr)
		return err
	}
	cfg.Logger.Info("Server listening", "address", lis.Addr().String())

	return cfg.Serve(ctx, lis)
}

// Serve serves with the given listener. It waits for shutdown signals with the
// given context and calls GracefulShutdown as configured. If the context is
// cancelled a hard shutdown is initiated.
func (cfg *ServerConfig) Serve(ctx context.Context, lis net.Listener) error {
	cfg.init()
	defer cfg.warnCancel()

	g, ctx := errgroup.WithContext(ctx)
	warnCtx, softCtx, hardCtx := Contexts(ctx, cfg.Logger, cfg.ShutdownDelay, cfg.ShutdownGrace)
	// Serve.
	g.Go(func() error {
		defer lis.Close()
		if err := cfg.Server.Serve(lis); err != nil {
			cfg.Logger.Error(err, "Server failed")
			return err
		}
		return nil
	})
	// Signal shutting down.
	g.Go(func() error {
		<-warnCtx.Done()
		cfg.warnCancel()
		return nil
	})
	// Watch for shutdown signal.
	g.Go(func() error {
		<-softCtx.Done()
		if err := cfg.Server.GracefulShutdown(hardCtx); err != nil {
			cfg.Logger.Error(err, "Server graceful shutdown failed")
			return err
		}
		return nil
	})
	// Wait for shutdown.
	return g.Wait()
}

// DualServerConfig specifies a server split between internal and external
// clients with graceful shutdown parameters.
type DualServerConfig struct {
	_ struct{}

	// ExternalServer is the server for primary clients.
	ExternalServer Server

	// InternalServer is the server for health checks, metrics, debugging,
	// profiling, etc. It shuts down after the ExternalServer.
	InternalServer Server

	// ShutdownDelay gives time for load balancers to remove the server from
	// their backend pools after a shutdown signal is received and before it
	// stops listening.
	ShutdownDelay time.Duration

	// ShutdownGrace gives time for pending requests to complete before the
	// server forcibly shuts down.
	ShutdownGrace time.Duration

	// Logger optionally adds the ability to log messages, both errors and not.
	Logger logr.Logger

	initOnce   sync.Once
	warnCtx    context.Context
	warnCancel context.CancelFunc
}

func (cfg *DualServerConfig) init() {
	cfg.initOnce.Do(func() {
		cfg.Logger = loggerOrDiscard(cfg.Logger)
		cfg.warnCtx, cfg.warnCancel = context.WithCancel(context.TODO())
	})
}

// ShuttingDown returns a channel that is closed when the server encounters
// an error or it receives its first signal to begin shutting down.
func (cfg *DualServerConfig) ShuttingDown() <-chan struct{} {
	cfg.init()
	return cfg.warnCtx.Done()
}

// ListenAndServe listens on the given addresses and calls Serve.
func (cfg *DualServerConfig) ListenAndServe(ctx context.Context, intAddr, extAddr string) error {
	cfg.init()
	defer cfg.warnCancel()

	intLis, err := net.Listen("tcp", intAddr)
	if err != nil {
		cfg.Logger.Error(err, "Internal server failed to listen", "address", intAddr)
		return err
	}
	cfg.Logger.Info("Internal server listening", "address", intLis.Addr().String())

	extLis, err := net.Listen("tcp", extAddr)
	if err != nil {
		cfg.Logger.Error(err, "External server failed to listen", "address", extAddr)
		intLis.Close()
		return err
	}
	cfg.Logger.Info("External server listening", "address", extLis.Addr().String())

	return cfg.Serve(ctx, intLis, extLis)
}

// Serve serves with the given listeners. It waits for shutdown signals with the
// given context and calls GracefulShutdown as configured. If the context is cancelled
// a hard shutdown is initiated.
func (cfg *DualServerConfig) Serve(ctx context.Context, intLis, extLis net.Listener) error {
	cfg.init()
	defer cfg.warnCancel()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)
	warnCtx, softCtx, hardCtx := Contexts(ctx, cfg.Logger, cfg.ShutdownDelay, cfg.ShutdownGrace)
	extShutdownDone := make(chan struct{})

	// Serve internal.
	g.Go(func() error {
		defer intLis.Close()
		if err := cfg.InternalServer.Serve(intLis); err != nil {
			cfg.Logger.Error(err, "Internal server failed")
			return err
		}
		return nil
	})
	// Serve external.
	g.Go(func() error {
		defer extLis.Close()
		if err := cfg.ExternalServer.Serve(extLis); err != nil {
			cfg.Logger.Error(err, "External server failed")
			return err
		}
		return nil
	})
	// Signal shutting down.
	g.Go(func() error {
		<-warnCtx.Done()
		cfg.warnCancel()
		return nil
	})
	// Watch for signal and shutdown external first.
	g.Go(func() error {
		defer close(extShutdownDone)
		<-softCtx.Done()
		if err := cfg.ExternalServer.GracefulShutdown(hardCtx); err != nil {
			cfg.Logger.Error(err, "External server graceful shutdown failed")
			return err
		}
		return nil
	})
	// Shutdown internal after external.
	g.Go(func() error {
		<-extShutdownDone
		if err := cfg.InternalServer.GracefulShutdown(hardCtx); err != nil {
			cfg.Logger.Error(err, "Internal server graceful shutdown failed")
			return err
		}
		return nil
	})
	// Wait for shutdown.
	return g.Wait()
}

func loggerOrDiscard(log logr.Logger) logr.Logger {
	if log.GetSink() == nil {
		return logr.Discard()
	}
	return log
}
