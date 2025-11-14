package endpoint

import (
	"context"

	"waverless/internal/model"
	"waverless/pkg/store/mysql"
	redisstore "waverless/pkg/store/redis"
)

type endpointRepository interface {
	Create(ctx context.Context, endpoint *mysql.Endpoint) error
	Get(ctx context.Context, name string) (*mysql.Endpoint, error)
	Update(ctx context.Context, endpoint *mysql.Endpoint) error
	Delete(ctx context.Context, name string) error
	List(ctx context.Context) ([]*mysql.Endpoint, error)
}

type autoscalerConfigRepository interface {
	Get(ctx context.Context, endpoint string) (*mysql.AutoscalerConfig, error)
	CreateOrUpdate(ctx context.Context, cfg *mysql.AutoscalerConfig) error
}

type taskRepository interface {
	CountByEndpointAndStatus(ctx context.Context, endpoint, status string) (int64, error)
	GetInProgressTasks(ctx context.Context) ([]string, error)
	Get(ctx context.Context, taskID string) (*mysql.Task, error)
}

type workerRepository interface {
	GetAll(ctx context.Context) ([]*model.Worker, error)
}

// compile-time assertions

var (
	_ endpointRepository         = (*mysql.EndpointRepository)(nil)
	_ autoscalerConfigRepository = (*mysql.AutoscalerConfigRepository)(nil)
	_ taskRepository             = (*mysql.TaskRepository)(nil)
	_ workerRepository           = (*redisstore.WorkerRepository)(nil)
)
