// Package modesource subscribes to the mode server over gRPC.
package modesource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/go-logr/logr"
	"google.golang.org/grpc"

	modev1 "github.com/example/modeswitch/gen/modeswitch/v1"
)

// Source is a controller-runtime Runnable. It only runs on the leader.
//
// Failure behaviour: if the stream drops, the last known mode is held
// (fail-static) and the controller keeps enforcing it while reconnecting.
type Source struct {
	Addr     string
	DialOpts []grpc.DialOption
	Known    map[string]bool

	Set      func(mode string) (changed bool)
	Current  func() (string, bool)
	OnChange func(ctx context.Context, mode string) error

	// Resync re-enqueues all workloads periodically while a mode is known,
	// covering any missed enqueue. 0 disables.
	Resync time.Duration
	Log    logr.Logger
}

func (s *Source) NeedLeaderElection() bool { return true }

func (s *Source) Start(ctx context.Context) error {
	conn, err := grpc.NewClient(s.Addr, s.DialOpts...)
	if err != nil {
		return fmt.Errorf("grpc client %s: %w", s.Addr, err)
	}
	defer conn.Close()
	c := modev1.NewModeServiceClient(conn)

	if s.Resync > 0 {
		go s.resyncLoop(ctx)
	}

	const minBackoff, maxBackoff = time.Second, 30 * time.Second
	backoff := minBackoff
	for {
		received, err := s.watch(ctx, c)
		if ctx.Err() != nil {
			return nil
		}
		if received {
			backoff = minBackoff
		}
		s.Log.Error(err, "mode stream ended; holding last known mode", "retryIn", backoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

// watch is the only function that touches generated types; adapt it to
// your application's real proto.
func (s *Source) watch(ctx context.Context, c modev1.ModeServiceClient) (bool, error) {
	stream, err := c.WatchMode(ctx, &modev1.WatchModeRequest{})
	if err != nil {
		return false, err
	}
	received := false
	for {
		upd, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = errors.New("server closed stream")
			}
			return received, err
		}
		received = true
		s.handle(ctx, upd.GetMode())
	}
}

func (s *Source) handle(ctx context.Context, mode string) {
	if !s.Known[mode] {
		s.Log.Info("ignoring unknown mode", "mode", mode)
		return
	}
	if !s.Set(mode) {
		return
	}
	s.Log.Info("mode changed", "mode", mode)
	if err := s.OnChange(ctx, mode); err != nil {
		s.Log.Error(err, "enqueue after mode change failed; resync will retry")
	}
}

func (s *Source) resyncLoop(ctx context.Context) {
	t := time.NewTicker(s.Resync)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if mode, ok := s.Current(); ok {
				if err := s.OnChange(ctx, mode); err != nil {
					s.Log.Error(err, "resync failed")
				}
			}
		}
	}
}
