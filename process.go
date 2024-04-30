// SPDX-License-Identifier: MIT
//
// Copyright 2023 Andrew Bursavich. All rights reserved.
// Use of this source code is governed by The MIT License
// which can be found in the LICENSE file.

package graceful

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
)

const (
	DefaultDelay = time.Duration(0)
	DefaultGrace = 10 * time.Second
)

// ErrUnexpectedEnd means that a process ended unexpectedly without returning
// an error before Shutdown was called.
var ErrUnexpectedEnd = errors.New("graceful: process ended unexpectedly")

// A Process is a unit of execution that runs indefinitely until it's signaled to stop.
type Process interface {
	// Run starts the process with the given context and waits until it's stopped.
	// It returns an error if the process cannot be started or fails unexpectedly.
	// It does not return an error if it's stopped by the Shutdown method.
	Run(context.Context) error

	// Shutdown stops the process with the given context.
	// It stops accepting new work and waits for any pending work to finish.
	// If the context is cancelled, it abandons any pending work and returns an error.
	Shutdown(context.Context) error
}

// An OrderedGroup is a composition of Processes.
// All processes in the group are started concurrently but they're stopped serially.
type OrderedGroup []Process

func (procs OrderedGroup) Run(ctx context.Context) error {
	var grp errgroup.Group
	for _, p := range procs {
		p := p
		grp.Go(func() error { return p.Run(ctx) })
	}
	return grp.Wait()
}

func (procs OrderedGroup) Shutdown(ctx context.Context) error {
	var errs []error
	for _, p := range procs {
		if err := p.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// A ConcurrentGroup is a composition of Processes.
// All processes in the group are started and stopped concurrently.
type ConcurrentGroup []Process

func (procs ConcurrentGroup) Run(ctx context.Context) error {
	var grp errgroup.Group
	for _, p := range procs {
		p := p
		grp.Go(func() error { return p.Run(ctx) })
	}
	return grp.Wait()
}

func (procs ConcurrentGroup) Shutdown(ctx context.Context) error {
	var grp errgroup.Group
	for _, p := range procs {
		p := p
		grp.Go(func() error { return p.Shutdown(ctx) })
	}
	return grp.Wait()
}

type config struct {
	log    logr.Logger
	delay  time.Duration
	grace  time.Duration
	notify []func()
}

type optionFunc func(*config) error

func (fn optionFunc) apply(c *config) error { return fn(c) }

// An Option overrides a default behavior.
type Option interface {
	apply(*config) error
}

// WithLogger returns an Option that sets a logger.
func WithLogger(log logr.Logger) Option {
	return optionFunc(func(c *config) error {
		c.log = log
		return nil
	})
}

// WithDelay returns an Option that sets the shutdown delay period.
//
// For a server, this gives time for clients and load balancers to remove it from their
// backend pools after a shutdown signal is received and before it stops listening.
func WithDelay(delay time.Duration) Option {
	return optionFunc(func(c *config) error {
		if delay < 0 {
			return errors.New("graceful.WithDelay: delay cannot be negative")
		}
		c.delay = delay
		return nil
	})
}

// WithGrace returns an Option that sets the shutdown grace period.
//
// For a server, this gives time for pending requests to complete before it forcibly exits.
func WithGrace(grace time.Duration) Option {
	return optionFunc(func(c *config) error {
		if grace < 0 {
			return errors.New("graceful.WithGrace: grace cannot be negative")
		}
		c.grace = grace
		return nil
	})
}

// WithNotifyFunc returns an Option that adds the given notify function to a list
// of those that will be called when the shutdown process is initially triggered.
// It will be called before the shutdown delay.
//
// For a server, this gives it time to pre-emptively fail health checks or notify
// clients that it will be going away.
func WithNotifyFunc(notify func()) Option {
	return optionFunc(func(c *config) error {
		if notify == nil {
			return errors.New("graceful.WithNotifyFunc: notify cannot be nil")
		}
		c.notify = append(c.notify, notify)
		return nil
	})
}

// Run executes the process with the given context and options.
func Run(ctx context.Context, process Process, options ...Option) error {
	cfg := config{
		delay: DefaultDelay,
		grace: DefaultGrace,
	}
	for _, o := range options {
		if err := o.apply(&cfg); err != nil {
			return err
		}
	}

	g, ctx := errgroup.WithContext(ctx)
	warnCtx, softCtx, hardCtx := Contexts(ctx, cfg.log, cfg.delay, cfg.grace)
	// Run the process.
	// NOTE: Returning an error will close the context and trigger shutdown steps.
	g.Go(func() error {
		if err := process.Run(hardCtx); err != nil {
			cfg.log.Error(err, "Process failed")
			return err
		}
		select {
		case <-softCtx.Done():
			return nil
		default:
			cfg.log.Info("Process exited without being stopped")
			return ErrUnexpectedEnd
		}
	})
	// Wait for error or shutdown signal.
	g.Go(func() error {
		// Notify impending shutdown.
		<-warnCtx.Done()
		for _, fn := range cfg.notify {
			fn()
		}
		// Shutdown the process.
		<-softCtx.Done()
		if err := process.Shutdown(hardCtx); err != nil {
			cfg.log.Error(err, "Process shutdown failed")
			return err
		}
		return nil
	})
	// Wait for shutdown.
	return g.Wait()
}

type funcProcess struct {
	fn func(context.Context) error

	closeStopping sync.Once
	stopping      chan struct{}

	closeStopped sync.Once
	stopped      chan struct{}
}

// FuncProcess converts a function into a graceful.Process.
// When the context is cancelled, fn should return a nil error.
func FuncProcess(fn func(context.Context) error) Process {
	return &funcProcess{
		fn:       fn,
		stopping: make(chan struct{}),
		stopped:  make(chan struct{}),
	}
}

func (f *funcProcess) Run(ctx context.Context) error {
	defer f.closeStopped.Do(func() { close(f.stopped) })
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-f.stopping:
		case <-f.stopped:
		}
		cancel()
	}()
	return f.fn(ctx)
}

func (f *funcProcess) Shutdown(ctx context.Context) error {
	f.closeStopping.Do(func() { close(f.stopping) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.stopped:
		return nil
	}
}
