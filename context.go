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

// Contexts returns two contexts which respectively serve as soft and hard
// shutdown signals. They are cancelled after TERM or INT signals are received.
//
// If delay is positive, it will wait that duration after receiving a signal
// before cancelling the first context. This is useful to allow loadbalancer
// updates before the server stops accepting new requests.
//
// If grace is positive, it will wait that duration after cancelling the first
// context before cancelling the second context. This is useful to set a maximum
// time to allow pending requests to complete.
//
// Repeated TERM or INT signals will bypass any delay or grace time.
func Contexts(ctx context.Context, log logr.Logger, delay, grace time.Duration) (context.Context, context.Context) {
	if log == nil {
		log = logr.Discard()
	}
	sigCh := make(chan os.Signal, 3)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	hardCtx, hardCancel := context.WithCancel(ctx)
	softCtx, softCancel := context.WithCancel(hardCtx)
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
	return softCtx, hardCtx
}
