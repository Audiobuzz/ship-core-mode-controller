package controller

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// Broadcaster enqueues every opted-in workload when the mode changes.
type Broadcaster struct {
	Client            client.Reader
	DeploymentEvents  chan<- event.GenericEvent
	StatefulSetEvents chan<- event.GenericEvent
}

func (b *Broadcaster) EnqueueAll(ctx context.Context) error {
	sel := client.MatchingLabels{LabelEnabled: "true"}

	var deps appsv1.DeploymentList
	if err := b.Client.List(ctx, &deps, sel); err != nil {
		return err
	}
	for i := range deps.Items {
		if err := send(ctx, b.DeploymentEvents, &deps.Items[i]); err != nil {
			return err
		}
	}

	var stss appsv1.StatefulSetList
	if err := b.Client.List(ctx, &stss, sel); err != nil {
		return err
	}
	for i := range stss.Items {
		if err := send(ctx, b.StatefulSetEvents, &stss.Items[i]); err != nil {
			return err
		}
	}
	return nil
}

func send(ctx context.Context, ch chan<- event.GenericEvent, obj client.Object) error {
	select {
	case ch <- event.GenericEvent{Object: obj}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
