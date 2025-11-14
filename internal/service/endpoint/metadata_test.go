package endpoint

import (
	"context"
	"testing"
	"time"

	"waverless/internal/model"
	"waverless/pkg/interfaces"
	"waverless/pkg/store/mysql"

	"github.com/stretchr/testify/require"
)

func TestMetadataManagerSaveCreatesEndpointAndConfig(t *testing.T) {
	ctx := context.Background()
	endpointRepo := newFakeEndpointRepo()
	configRepo := newFakeAutoscalerConfigRepo()
	taskRepo := newFakeTaskRepo()
	workerRepo := newFakeWorkerRepo()

	manager := NewMetadataManager(endpointRepo, configRepo, taskRepo, workerRepo)

	meta := &interfaces.EndpointMetadata{
		Name:        "demo",
		SpecName:    "tiny",
		Image:       "demo:v1",
		Replicas:    1,
		MaxReplicas: 5,
	}

	require.NoError(t, manager.Save(ctx, meta))

	savedEndpoint, err := endpointRepo.Get(ctx, "demo")
	require.NoError(t, err)
	require.NotNil(t, savedEndpoint)
	require.Equal(t, "demo", savedEndpoint.Endpoint)
	require.Equal(t, "tiny", savedEndpoint.SpecName)

	savedConfig, err := configRepo.Get(ctx, "demo")
	require.NoError(t, err)
	require.NotNil(t, savedConfig)
	require.Equal(t, 5, savedConfig.MaxReplicas)
	require.True(t, savedConfig.EnableDynamicPrio)
}

func TestMetadataManagerGetStatsAggregatesData(t *testing.T) {
	ctx := context.Background()
	endpointRepo := newFakeEndpointRepo()
	configRepo := newFakeAutoscalerConfigRepo()
	taskRepo := newFakeTaskRepo()
	workerRepo := newFakeWorkerRepo()

	manager := NewMetadataManager(endpointRepo, configRepo, taskRepo, workerRepo)

	_ = endpointRepo.Create(ctx, &mysql.Endpoint{
		Endpoint: "demo",
		SpecName: "tiny",
		Image:    "demo:v1",
		Replicas: 1,
		Status:   "active",
	})

	taskRepo.withCount("demo", string(model.TaskStatusPending), 3)
	taskRepo.withCount("demo", string(model.TaskStatusInProgress), 2)

	workerRepo.workers = []*model.Worker{
		{ID: "w1", Endpoint: "demo", Status: model.WorkerStatusOnline},
		{ID: "w2", Endpoint: "demo", Status: model.WorkerStatusBusy},
	}

	stats, err := manager.GetStats(ctx, "demo")
	require.NoError(t, err)
	require.Equal(t, 3, stats.PendingTasks)
	require.Equal(t, 2, stats.RunningTasks)
	require.Equal(t, 2, stats.TotalWorkers)
	require.Equal(t, 1, stats.BusyWorkers)
	require.Equal(t, 1, stats.OnlineWorkers)
}

// --- Fakes ---

type fakeEndpointRepo struct {
	items map[string]*mysql.Endpoint
}

func newFakeEndpointRepo() *fakeEndpointRepo {
	return &fakeEndpointRepo{items: make(map[string]*mysql.Endpoint)}
}

func (f *fakeEndpointRepo) Create(ctx context.Context, endpoint *mysql.Endpoint) error {
	cp := *endpoint
	f.items[endpoint.Endpoint] = &cp
	return nil
}

func (f *fakeEndpointRepo) Get(ctx context.Context, name string) (*mysql.Endpoint, error) {
	if ep, ok := f.items[name]; ok {
		cp := *ep
		return &cp, nil
	}
	return nil, nil
}

func (f *fakeEndpointRepo) Update(ctx context.Context, endpoint *mysql.Endpoint) error {
	cp := *endpoint
	f.items[endpoint.Endpoint] = &cp
	return nil
}

func (f *fakeEndpointRepo) Delete(ctx context.Context, name string) error {
	if ep, ok := f.items[name]; ok {
		ep.Status = "deleted"
	}
	return nil
}

func (f *fakeEndpointRepo) List(ctx context.Context) ([]*mysql.Endpoint, error) {
	result := make([]*mysql.Endpoint, 0, len(f.items))
	for _, ep := range f.items {
		cp := *ep
		result = append(result, &cp)
	}
	return result, nil
}

type fakeAutoscalerConfigRepo struct {
	items map[string]*mysql.AutoscalerConfig
}

func newFakeAutoscalerConfigRepo() *fakeAutoscalerConfigRepo {
	return &fakeAutoscalerConfigRepo{items: make(map[string]*mysql.AutoscalerConfig)}
}

func (f *fakeAutoscalerConfigRepo) Get(ctx context.Context, endpoint string) (*mysql.AutoscalerConfig, error) {
	if cfg, ok := f.items[endpoint]; ok {
		cp := *cfg
		return &cp, nil
	}
	return nil, nil
}

func (f *fakeAutoscalerConfigRepo) CreateOrUpdate(ctx context.Context, cfg *mysql.AutoscalerConfig) error {
	cp := *cfg
	f.items[cfg.Endpoint] = &cp
	return nil
}

type fakeTaskRepo struct {
	counts map[string]map[string]int64
}

func newFakeTaskRepo() *fakeTaskRepo {
	return &fakeTaskRepo{counts: make(map[string]map[string]int64)}
}

func (f *fakeTaskRepo) withCount(endpoint, status string, count int64) {
	if _, ok := f.counts[endpoint]; !ok {
		f.counts[endpoint] = make(map[string]int64)
	}
	f.counts[endpoint][status] = count
}

func (f *fakeTaskRepo) CountByEndpointAndStatus(ctx context.Context, endpoint, status string) (int64, error) {
	if st, ok := f.counts[endpoint]; ok {
		return st[status], nil
	}
	return 0, nil
}

func (f *fakeTaskRepo) GetInProgressTasks(ctx context.Context) ([]string, error) {
	return []string{}, nil
}

type fakeWorkerRepo struct {
	workers []*model.Worker
}

func newFakeWorkerRepo() *fakeWorkerRepo {
	return &fakeWorkerRepo{workers: []*model.Worker{}}
}

func (f *fakeWorkerRepo) GetAll(ctx context.Context) ([]*model.Worker, error) {
	result := make([]*model.Worker, 0, len(f.workers))
	for _, w := range f.workers {
		cp := *w
		result = append(result, &cp)
	}
	return result, nil
}

// Unused methods to satisfy compiler (no-ops)
func (f *fakeTaskRepo) Create(ctx context.Context, task *mysql.Task) error          { return nil }
func (f *fakeTaskRepo) Get(ctx context.Context, taskID string) (*mysql.Task, error) { return nil, nil }
func (f *fakeTaskRepo) Update(ctx context.Context, task *mysql.Task) error          { return nil }
func (f *fakeWorkerRepo) Save(ctx context.Context, worker *model.Worker) error      { return nil }
func (f *fakeWorkerRepo) UpdateHeartbeat(ctx context.Context, workerID, endpoint string, jobs []string) error {
	return nil
}
func (f *fakeWorkerRepo) Delete(ctx context.Context, workerID string) error { return nil }
func (f *fakeWorkerRepo) Get(ctx context.Context, workerID string) (*model.Worker, error) {
	return nil, nil
}
func (f *fakeWorkerRepo) GetOnlineWorkerCount(ctx context.Context) (int, error) {
	return len(f.workers), nil
}
func (f *fakeWorkerRepo) AddToPendingQueue(ctx context.Context, endpoint, taskID string) error {
	return nil
}
func (f *fakeWorkerRepo) PullFromPendingQueue(ctx context.Context, endpoint string, count int) ([]string, error) {
	return nil, nil
}
func (f *fakeWorkerRepo) AssignTaskToWorker(ctx context.Context, workerID, taskID string) error {
	return nil
}
func (f *fakeWorkerRepo) GetPendingQueueLength(ctx context.Context, endpoint string) (int64, error) {
	return 0, nil
}
func (f *fakeWorkerRepo) GetAssignedTasks(ctx context.Context, workerID string) ([]string, error) {
	return nil, nil
}
func (f *fakeWorkerRepo) RemoveTaskFromWorker(ctx context.Context, workerID, taskID string) error {
	return nil
}
func (f *fakeWorkerRepo) ClearWorkerTasks(ctx context.Context, workerID string) error { return nil }
func (f *fakeWorkerRepo) GetByEndpoint(ctx context.Context, endpoint string) ([]*model.Worker, error) {
	return f.GetAll(ctx)
}
func (f *fakeWorkerRepo) CountByEndpoint(ctx context.Context, endpoint string) (int, error) {
	return len(f.workers), nil
}
func (f *fakeWorkerRepo) GetBusyWorkers(ctx context.Context, endpoint string) ([]*model.Worker, error) {
	return nil, nil
}
func (f *fakeWorkerRepo) GetOfflineWorkers(ctx context.Context, timeout time.Duration) ([]*model.Worker, error) {
	return nil, nil
}
