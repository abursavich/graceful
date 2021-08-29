// SPDX-License-Identifier: MIT
//
// Copyright 2021 Andrew Bursavich. All rights reserved.
// Use of this source code is governed by The MIT License
// which can be found in the LICENSE file.

package graceful

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-logr/logr"
)

// Contexts returns three contexts which respectively serve as warning, soft,
// and hard shutdown signals. They are cancelled after TERM or INT signals are
// received.
//
// When a shutdown signal is received, the warning context is cancelled. This is
// useful to start failing health checks while other traffic is still served.
//
// If delay is positive, the soft context will be cancelled after that duration.
// This is useful to allow loadbalancer updates before the server stops accepting
// new requests.
//
// If grace is positive, the hard context will be cancelled that duration after
// the soft context is cancelled. This is useful to set a maximum time to allow
// pending requests to complete.
//
// Repeated TERM or INT signals will bypass any delay or grace time.
func Contexts(ctx context.Context, log logr.Logger, delay, grace time.Duration) (warn, soft, hard context.Context) {
	sigCh := make(chan os.Signal, 3)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	hardCtx, hardCancel := context.WithCancel(ctx)
	softCtx, softCancel := context.WithCancel(hardCtx)
	warnCtx, warnCancel := context.WithCancel(softCtx)
	go func() {
		defer signal.Stop(sigCh)
		defer hardCancel()
		select {
		case sig := <-sigCh:
			log.Info("Shutdown triggered", "signal", sig.String())
		case <-ctx.Done():
			return
		}
		if delay > 0 {
			log.Info("Shutdown starting after delay period", "duration", delay)
			warnCancel()
			select {
			case <-time.After(delay):
				log.Info("Shutdown delay period ended")
			case sig := <-sigCh:
				log.Info("Skipping shutdown delay period", "signal", sig.String())
			case <-ctx.Done():
				return
			}
		}
		if grace > 0 {
			log.Info("Shutdown starting with grace period", "duration", grace)
			softCancel()
			select {
			case <-time.After(grace):
				log.Info("Shutdown grace period ended")
			case sig := <-sigCh:
				log.Info("Skipping shutdown grace period", "signal", sig.String())
			case <-ctx.Done():
				return
			}
		}
	}()
	return warnCtx, softCtx, hardCtx
}
