package main

import (
	"context"
	"log/slog"
	"time"

	actions "github.com/aaronhdez/kt_game-integration-api/Actions"
)

// pendingResolver is what the loop needs from
// actions.ResolvePendingCommandHandler. It is declared here, where it is
// used, so the loop can be tested with a stub.
type pendingResolver interface {
	Execute(ctx context.Context) (actions.ResolveSummary, error)
}

// runPendingResolver starts the pending resolver in its own goroutine: one
// run straight away (it resolves what a crash or a restart left PENDING) and
// then one every interval, until ctx is done. The returned channel is closed
// when the goroutine has finished, the run in progress included, so the
// caller can wait for it before closing the database.
//
// Cancelling ctx also cancels a Wallet call in progress. That outcome is
// unknown, so the transaction simply stays PENDING for the next start.
func runPendingResolver(ctx context.Context, resolver pendingResolver, interval time.Duration, logger *slog.Logger) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		logger.Info("pending resolver started", "interval", interval.String())
		for {
			resolvePendingOnce(ctx, resolver, logger)
			select {
			case <-ctx.Done():
				logger.Info("pending resolver stopped")
				return
			case <-ticker.C:
			}
		}
	}()
	return done
}

// resolvePendingOnce runs the resolver and writes one line with its summary:
// at info level if it found something to re-send, at debug level otherwise,
// so an idle service does not fill the log.
func resolvePendingOnce(ctx context.Context, resolver pendingResolver, logger *slog.Logger) {
	summary, err := resolver.Execute(ctx)
	if err != nil && ctx.Err() == nil {
		// A cancelled ctx is a shutdown, not a failure.
		logger.Error("pending resolver run failed", "error", err.Error())
	}

	level := slog.LevelDebug
	if summary.Checked > 0 {
		level = slog.LevelInfo
	}
	logger.Log(context.Background(), level, "pending resolver run",
		"checked", summary.Checked,
		"completed", summary.Completed,
		"failed", summary.Failed,
		"still_pending", summary.StillPending,
		"errors", summary.Errors,
	)
}
