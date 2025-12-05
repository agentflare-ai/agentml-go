package bubbletea

import (
	"context"

	"github.com/agentflare-ai/agentml-go"
)

type fakeDispatcher struct {
	events []*agentml.Event
}

func newFakeDispatcher() *fakeDispatcher {
	return &fakeDispatcher{}
}

func (f *fakeDispatcher) Send(ctx context.Context, ev *agentml.Event) error {
	f.events = append(f.events, ev)
	return nil
}
