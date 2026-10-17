// Package jobs runs long operations (install, update, ...) one at a time in the background and
// fans their progress out to subscribers (the web UI's server-sent events).
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// States of a job.
const (
	Queued    = "queued"
	Running   = "running"
	Done      = "done"
	Failed    = "failed"
	Cancelled = "cancelled"
)

// LogLine is one message of a job.
type LogLine struct {
	Time time.Time `json:"t"`
	Msg  string    `json:"msg"`
}

// Job is one background operation.
type Job struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Title      string    `json:"title"`
	State      string    `json:"state"`
	Log        []LogLine `json:"log"`
	Result     any       `json:"result,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`

	run    func(ctx context.Context, logf func(string, ...any)) (any, error)
	cancel context.CancelFunc
}

// Event is what subscribers receive.
type Event struct {
	ID   int64           `json:"id"`
	Type string          `json:"type"` // job | log
	Data json.RawMessage `json:"data"`
}

// Manager queues jobs and runs them sequentially.
type Manager struct {
	mu      sync.Mutex
	jobs    map[string]*Job
	order   []string
	queue   chan *Job
	subs    map[chan Event]struct{}
	ring    []Event
	nextID  int64
	current *Job
	keep    int
}

// New starts a manager; it stops when ctx is done.
func New(ctx context.Context) *Manager {
	m := &Manager{jobs: map[string]*Job{}, queue: make(chan *Job, 64), subs: map[chan Event]struct{}{}, keep: 100}
	go m.loop(ctx)
	return m
}

// Submit queues a job.
func (m *Manager) Submit(kind, title string, run func(ctx context.Context, logf func(string, ...any)) (any, error)) (*Job, error) {
	j := &Job{ID: newID(), Kind: kind, Title: title, State: Queued, Log: []LogLine{}, CreatedAt: time.Now(), run: run}
	m.mu.Lock()
	m.jobs[j.ID] = j
	m.order = append(m.order, j.ID)
	for len(m.order) > m.keep {
		delete(m.jobs, m.order[0])
		m.order = m.order[1:]
	}
	m.mu.Unlock()
	select {
	case m.queue <- j:
	default:
		return nil, fmt.Errorf("too many queued jobs")
	}
	m.publishJob(j)
	return j, nil
}

func (m *Manager) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-m.queue:
			m.runJob(ctx, j)
		}
	}
}

func (m *Manager) runJob(parent context.Context, j *Job) {
	ctx, cancel := context.WithCancel(parent)
	m.mu.Lock()
	if j.State == Cancelled {
		m.mu.Unlock()
		cancel()
		return
	}
	j.State, j.StartedAt, j.cancel, m.current = Running, time.Now(), cancel, j
	m.mu.Unlock()
	m.publishJob(j)
	result, err := j.run(ctx, func(format string, args ...any) { m.Logf(j, format, args...) })
	cancelled := ctx.Err() != nil && parent.Err() == nil
	cancel()
	m.mu.Lock()
	j.FinishedAt, j.Result, m.current = time.Now(), result, nil
	switch {
	case cancelled && err != nil:
		j.State, j.Error = Cancelled, "cancelled"
	case err != nil:
		j.State, j.Error = Failed, err.Error()
	default:
		j.State = Done
	}
	m.mu.Unlock()
	m.publishJob(j)
}

// Logf appends a line to a job (or to the running job when j is nil) and publishes it.
func (m *Manager) Logf(j *Job, format string, args ...any) {
	m.mu.Lock()
	if j == nil {
		j = m.current
	}
	if j == nil {
		m.mu.Unlock()
		return
	}
	line := LogLine{Time: time.Now(), Msg: fmt.Sprintf(format, args...)}
	j.Log = append(j.Log, line)
	id := j.ID
	m.mu.Unlock()
	data, _ := json.Marshal(map[string]any{"job": id, "line": line})
	m.publish("log", data)
}

// Cancel stops a queued or running job.
func (m *Manager) Cancel(id string) bool {
	m.mu.Lock()
	j, ok := m.jobs[id]
	if !ok || (j.State != Queued && j.State != Running) {
		m.mu.Unlock()
		return false
	}
	if j.State == Queued {
		j.State, j.Error, j.FinishedAt = Cancelled, "cancelled", time.Now()
	}
	if j.cancel != nil {
		j.cancel()
	}
	m.mu.Unlock()
	m.publishJob(j)
	return true
}

// Get returns a snapshot of a job.
func (m *Manager) Get(id string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return Job{}, false
	}
	return copyJob(j), true
}

// List returns all kept jobs, newest first.
func (m *Manager) List() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Job, 0, len(m.order))
	for i := len(m.order) - 1; i >= 0; i-- {
		out = append(out, copyJob(m.jobs[m.order[i]]))
	}
	return out
}

func copyJob(j *Job) Job {
	c := *j
	c.Log = append([]LogLine(nil), j.Log...)
	c.run, c.cancel = nil, nil
	return c
}

func (m *Manager) publishJob(j *Job) {
	m.mu.Lock()
	c := copyJob(j)
	m.mu.Unlock()
	data, _ := json.Marshal(c)
	m.publish("job", data)
}

// Publish sends an arbitrary event (e.g. "snapshot") to subscribers.
func (m *Manager) Publish(typ string, v any) {
	data, _ := json.Marshal(v)
	m.publish(typ, data)
}

func (m *Manager) publish(typ string, data []byte) {
	m.mu.Lock()
	m.nextID++
	ev := Event{ID: m.nextID, Type: typ, Data: data}
	m.ring = append(m.ring, ev)
	if len(m.ring) > 500 {
		m.ring = m.ring[len(m.ring)-500:]
	}
	subs := make([]chan Event, 0, len(m.subs))
	for c := range m.subs {
		subs = append(subs, c)
	}
	m.mu.Unlock()
	for _, c := range subs {
		select {
		case c <- ev:
		default: // a slow subscriber misses events; it can reload the job list
		}
	}
}

// Subscribe returns a channel of events after lastID (replayed from the ring) and an
// unsubscribe function.
func (m *Manager) Subscribe(lastID int64) (<-chan Event, func()) {
	c := make(chan Event, 256)
	m.mu.Lock()
	for _, ev := range m.ring {
		if ev.ID > lastID && lastID > 0 {
			select {
			case c <- ev:
			default:
			}
		}
	}
	m.subs[c] = struct{}{}
	m.mu.Unlock()
	return c, func() {
		m.mu.Lock()
		delete(m.subs, c)
		m.mu.Unlock()
	}
}

// Wait blocks until the job is finished or ctx ends.
func (m *Manager) Wait(ctx context.Context, id string) (Job, error) {
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	for {
		j, ok := m.Get(id)
		if !ok {
			return Job{}, fmt.Errorf("no job %s", id)
		}
		if j.State == Done || j.State == Failed || j.State == Cancelled {
			return j, nil
		}
		select {
		case <-ctx.Done():
			return j, ctx.Err()
		case <-t.C:
		}
	}
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}
