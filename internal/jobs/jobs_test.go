package jobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestJobsRunInOrderAndPublish(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := New(ctx)
	events, unsub := m.Subscribe(0)
	defer unsub()
	j1, _ := m.Submit("install", "one", func(ctx context.Context, logf func(string, ...any)) (any, error) {
		logf("working on %s", "one")
		return map[string]int{"n": 1}, nil
	})
	j2, _ := m.Submit("update", "two", func(ctx context.Context, logf func(string, ...any)) (any, error) {
		return nil, errors.New("boom")
	})
	got1, _ := m.Wait(ctx, j1.ID)
	got2, _ := m.Wait(ctx, j2.ID)
	if got1.State != Done || len(got1.Log) != 1 || got1.Log[0].Msg != "working on one" || got1.Result == nil {
		t.Errorf("%+v", got1)
	}
	if got2.State != Failed || got2.Error != "boom" {
		t.Errorf("%+v", got2)
	}
	if list := m.List(); len(list) != 2 || list[0].ID != j2.ID {
		t.Errorf("order %+v", list)
	}
	seen := map[string]int{}
	timeout := time.After(time.Second)
	for seen["job"] < 6 || seen["log"] < 1 {
		select {
		case ev := <-events:
			seen[ev.Type]++
		case <-timeout:
			t.Fatalf("events %v", seen)
		}
	}
	// Replay after an id.
	replay, unsub2 := m.Subscribe(1)
	defer unsub2()
	select {
	case ev := <-replay:
		if ev.ID != 2 {
			t.Errorf("replay starts at %d", ev.ID)
		}
	case <-time.After(time.Second):
		t.Error("no replay")
	}
}

func TestCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := New(ctx)
	started := make(chan struct{})
	j, _ := m.Submit("long", "long", func(ctx context.Context, logf func(string, ...any)) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	<-started
	if !m.Cancel(j.ID) {
		t.Fatal("cancel refused")
	}
	got, _ := m.Wait(ctx, j.ID)
	if got.State != Cancelled {
		t.Errorf("%+v", got)
	}
	if m.Cancel(j.ID) || m.Cancel("nope") {
		t.Error("cancelled twice")
	}
}
