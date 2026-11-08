package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	actions "github.com/aaronhdez/kt_game-integration-api/Actions"
)

// waitTimeout bounds every wait of these tests, so a broken loop fails the
// test instead of hanging it.
const waitTimeout = 2 * time.Second

func TestRunPendingResolver_RunsOnceStraightAway(t *testing.T) {
	resolver := newStubResolver(actions.ResolveSummary{}, nil)
	ctx := givenAStoppableContext(t)

	runPendingResolver(ctx, resolver, time.Hour, discardLogger())

	assertRuns(t, resolver, 1)
}

func TestRunPendingResolver_RunsAgainOnEveryTick(t *testing.T) {
	resolver := newStubResolver(actions.ResolveSummary{}, nil)
	ctx := givenAStoppableContext(t)

	runPendingResolver(ctx, resolver, 10*time.Millisecond, discardLogger())

	assertRuns(t, resolver, 3)
}

func TestRunPendingResolver_ContextDone_FinishesTheGoroutine(t *testing.T) {
	resolver := newStubResolver(actions.ResolveSummary{}, nil)
	ctx, stop := context.WithCancel(context.Background())
	done := runPendingResolver(ctx, resolver, time.Hour, discardLogger())

	stop()

	assertClosed(t, done)
}

func TestRunPendingResolver_RunFails_KeepsRunning(t *testing.T) {
	resolver := newStubResolver(actions.ResolveSummary{}, errors.New("database down"))
	ctx := givenAStoppableContext(t)

	runPendingResolver(ctx, resolver, 10*time.Millisecond, discardLogger())

	assertRuns(t, resolver, 2)
}

func TestRunPendingResolver_RunFails_LogsTheError(t *testing.T) {
	resolver := newStubResolver(actions.ResolveSummary{}, errors.New("database down"))

	output := runOnceAndStop(t, resolver, slog.LevelInfo)

	assertOutputContains(t, output, `"msg":"pending resolver run failed","error":"database down"`)
}

func TestRunPendingResolver_RunWithTransactions_LogsTheSummaryAtInfoLevel(t *testing.T) {
	resolver := newStubResolver(actions.ResolveSummary{Checked: 3, Completed: 1, Failed: 1, StillPending: 1}, nil)

	output := runOnceAndStop(t, resolver, slog.LevelInfo)

	assertOutputContains(t, output, `"level":"INFO","msg":"pending resolver run","checked":3,"completed":1,"failed":1,"still_pending":1,"errors":0`)
}

// An idle service would otherwise write a line every interval.
func TestRunPendingResolver_RunWithNothingPending_DoesNotLogAtInfoLevel(t *testing.T) {
	resolver := newStubResolver(actions.ResolveSummary{}, nil)

	output := runOnceAndStop(t, resolver, slog.LevelInfo)

	assertOutputNotContains(t, output, `"msg":"pending resolver run"`)
}

/////////////////////////////////////////////////////////////////////////////////////

// stubResolver returns a fixed result and signals every run on runs.
type stubResolver struct {
	summary actions.ResolveSummary
	err     error
	runs    chan struct{}
}

func newStubResolver(summary actions.ResolveSummary, err error) *stubResolver {
	return &stubResolver{summary: summary, err: err, runs: make(chan struct{}, 100)}
}

func (resolver *stubResolver) Execute(context.Context) (actions.ResolveSummary, error) {
	select {
	case resolver.runs <- struct{}{}:
	default: // the test has seen enough runs
	}
	return resolver.summary, resolver.err
}

// givenAStoppableContext is cancelled when the test ends, which stops the loop.
func givenAStoppableContext(t *testing.T) context.Context {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	return ctx
}

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// runOnceAndStop runs the loop until its first run has finished, stops it and
// returns what it logged. The log is read only after the goroutine is done.
func runOnceAndStop(t *testing.T, resolver *stubResolver, level slog.Level) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	ctx, stop := context.WithCancel(context.Background())
	done := runPendingResolver(ctx, resolver, time.Hour, NewLogger(&output, level))
	assertRuns(t, resolver, 1)
	stop()
	assertClosed(t, done)
	return &output
}

func assertRuns(t *testing.T, resolver *stubResolver, want int) {
	t.Helper()
	for i := range want {
		select {
		case <-resolver.runs:
		case <-time.After(waitTimeout):
			t.Fatalf("expected %d run(s), saw %d", want, i)
		}
	}
}

func assertClosed(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(waitTimeout):
		t.Fatal("expected the resolver goroutine to finish")
	}
}
