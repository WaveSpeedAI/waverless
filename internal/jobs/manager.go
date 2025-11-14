package jobs

import (
	"context"
	"sync"
	"time"

	"waverless/pkg/logger"
)

// Job represents a periodic background task.
type Job interface {
	Name() string
	Interval() time.Duration
	Run(ctx context.Context) error
}

// Manager orchestrates the lifecycle of background jobs.
type Manager struct {
	ctx     context.Context
	cancel  context.CancelFunc
	jobs    []Job
	started bool

	mu sync.Mutex
	wg sync.WaitGroup
}

// NewManager creates a job manager bound to the provided context.
func NewManager(parent context.Context) *Manager {
	ctx, cancel := context.WithCancel(parent)
	return &Manager{
		ctx:    ctx,
		cancel: cancel,
		jobs:   make([]Job, 0),
	}
}

// Register adds a job to the manager.
func (m *Manager) Register(job Job) {
	if job == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs = append(m.jobs, job)
}

// Start launches all registered jobs.
func (m *Manager) Start() {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	jobs := append([]Job(nil), m.jobs...)
	m.mu.Unlock()

	for _, job := range jobs {
		m.wg.Add(1)
		go m.runJob(job)
	}
}

// Stop signals all jobs to stop.
func (m *Manager) Stop() {
	m.cancel()
}

// Wait blocks until all jobs exit.
func (m *Manager) Wait() {
	m.wg.Wait()
}

func (m *Manager) runJob(job Job) {
	defer m.wg.Done()

	interval := job.Interval()
	if interval <= 0 {
		interval = time.Minute
	}

	// Run immediately once.
	m.executeJob(job)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.executeJob(job)
		}
	}
}

func (m *Manager) executeJob(job Job) {
	if err := job.Run(m.ctx); err != nil {
		logger.WarnCtx(m.ctx, "background job %s failed: %v", job.Name(), err)
	}
}
