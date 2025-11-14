package mysql

import (
	"waverless/internal/model"
	"waverless/pkg/interfaces"
)

// ToTaskDomain converts MySQL Task to domain Task model
func ToTaskDomain(mysqlTask *Task) *model.Task {
	if mysqlTask == nil {
		return nil
	}

	return &model.Task{
		ID:          mysqlTask.TaskID,
		Endpoint:    mysqlTask.Endpoint,
		Input:       map[string]interface{}(mysqlTask.Input),
		Status:      model.TaskStatus(mysqlTask.Status),
		Output:      map[string]interface{}(mysqlTask.Output),
		Error:       mysqlTask.Error,
		WorkerID:    mysqlTask.WorkerID,
		WebhookURL:  mysqlTask.WebhookURL,
		CreatedAt:   mysqlTask.CreatedAt,
		UpdatedAt:   mysqlTask.UpdatedAt,
		StartedAt:   mysqlTask.StartedAt,
		CompletedAt: mysqlTask.CompletedAt,
	}
}

// FromTaskDomain converts domain Task model to MySQL Task
func FromTaskDomain(domainTask *model.Task) *Task {
	if domainTask == nil {
		return nil
	}

	return &Task{
		TaskID:      domainTask.ID,
		Endpoint:    domainTask.Endpoint,
		Input:       JSONMap(domainTask.Input),
		Status:      string(domainTask.Status),
		Output:      JSONMap(domainTask.Output),
		Error:       domainTask.Error,
		WorkerID:    domainTask.WorkerID,
		WebhookURL:  domainTask.WebhookURL,
		CreatedAt:   domainTask.CreatedAt,
		UpdatedAt:   domainTask.UpdatedAt,
		StartedAt:   domainTask.StartedAt,
		CompletedAt: domainTask.CompletedAt,
	}
}

// ToAutoscalerConfigDomain converts MySQL AutoscalerConfig to domain EndpointConfig
func ToAutoscalerConfigDomain(mysqlConfig *AutoscalerConfig) *interfaces.EndpointConfig {
	if mysqlConfig == nil {
		return nil
	}

	return &interfaces.EndpointConfig{
		Name:              mysqlConfig.Endpoint,
		DisplayName:       mysqlConfig.DisplayName,
		SpecName:          mysqlConfig.SpecName,
		MinReplicas:       mysqlConfig.MinReplicas,
		MaxReplicas:       mysqlConfig.MaxReplicas,
		Replicas:          mysqlConfig.Replicas,
		ScaleUpThreshold:  mysqlConfig.ScaleUpThreshold,
		ScaleDownIdleTime: mysqlConfig.ScaleDownIdleTime,
		ScaleUpCooldown:   mysqlConfig.ScaleUpCooldown,
		ScaleDownCooldown: mysqlConfig.ScaleDownCooldown,
		Priority:          mysqlConfig.Priority,
		EnableDynamicPrio: mysqlConfig.EnableDynamicPrio,
		HighLoadThreshold: mysqlConfig.HighLoadThreshold,
		PriorityBoost:     mysqlConfig.PriorityBoost,
		// Note: Runtime state fields are not stored in MySQL
	}
}

// FromAutoscalerConfigDomain converts domain EndpointConfig to MySQL AutoscalerConfig
func FromAutoscalerConfigDomain(domainConfig *interfaces.EndpointConfig) *AutoscalerConfig {
	if domainConfig == nil {
		return nil
	}

	return &AutoscalerConfig{
		Endpoint:          domainConfig.Name,
		DisplayName:       domainConfig.DisplayName,
		SpecName:          domainConfig.SpecName,
		MinReplicas:       domainConfig.MinReplicas,
		MaxReplicas:       domainConfig.MaxReplicas,
		Replicas:          domainConfig.Replicas,
		ScaleUpThreshold:  domainConfig.ScaleUpThreshold,
		ScaleDownIdleTime: domainConfig.ScaleDownIdleTime,
		ScaleUpCooldown:   domainConfig.ScaleUpCooldown,
		ScaleDownCooldown: domainConfig.ScaleDownCooldown,
		Priority:          domainConfig.Priority,
		EnableDynamicPrio: domainConfig.EnableDynamicPrio,
		HighLoadThreshold: domainConfig.HighLoadThreshold,
		PriorityBoost:     domainConfig.PriorityBoost,
		Enabled:           true, // Default enabled
	}
}

// ToScalingEventDomain converts MySQL ScalingEvent to domain ScalingEvent
func ToScalingEventDomain(mysqlEvent *ScalingEvent) *interfaces.ScalingEvent {
	if mysqlEvent == nil {
		return nil
	}

	return &interfaces.ScalingEvent{
		ID:            mysqlEvent.EventID,
		Endpoint:      mysqlEvent.Endpoint,
		Timestamp:     mysqlEvent.Timestamp,
		Action:        mysqlEvent.Action,
		FromReplicas:  mysqlEvent.FromReplicas,
		ToReplicas:    mysqlEvent.ToReplicas,
		Reason:        mysqlEvent.Reason,
		QueueLength:   mysqlEvent.QueueLength,
		Priority:      mysqlEvent.Priority,
		PreemptedFrom: []string(mysqlEvent.PreemptedFrom),
	}
}

// FromScalingEventDomain converts domain ScalingEvent to MySQL ScalingEvent
func FromScalingEventDomain(domainEvent *interfaces.ScalingEvent) *ScalingEvent {
	if domainEvent == nil {
		return nil
	}

	return &ScalingEvent{
		EventID:       domainEvent.ID,
		Endpoint:      domainEvent.Endpoint,
		Timestamp:     domainEvent.Timestamp,
		Action:        domainEvent.Action,
		FromReplicas:  domainEvent.FromReplicas,
		ToReplicas:    domainEvent.ToReplicas,
		Reason:        domainEvent.Reason,
		QueueLength:   domainEvent.QueueLength,
		Priority:      domainEvent.Priority,
		PreemptedFrom: JSONStringArray(domainEvent.PreemptedFrom),
	}
}

// Batch conversion helpers

// ToTaskDomainList converts a list of MySQL tasks to domain tasks
func ToTaskDomainList(mysqlTasks []*Task) []*model.Task {
	if mysqlTasks == nil {
		return nil
	}

	domainTasks := make([]*model.Task, 0, len(mysqlTasks))
	for _, mysqlTask := range mysqlTasks {
		domainTasks = append(domainTasks, ToTaskDomain(mysqlTask))
	}
	return domainTasks
}

// ToAutoscalerConfigDomainList converts a list of MySQL configs to domain configs
func ToAutoscalerConfigDomainList(mysqlConfigs []*AutoscalerConfig) []*interfaces.EndpointConfig {
	if mysqlConfigs == nil {
		return nil
	}

	domainConfigs := make([]*interfaces.EndpointConfig, 0, len(mysqlConfigs))
	for _, mysqlConfig := range mysqlConfigs {
		domainConfigs = append(domainConfigs, ToAutoscalerConfigDomain(mysqlConfig))
	}
	return domainConfigs
}

// ToScalingEventDomainList converts a list of MySQL events to domain events
func ToScalingEventDomainList(mysqlEvents []*ScalingEvent) []*interfaces.ScalingEvent {
	if mysqlEvents == nil {
		return nil
	}

	domainEvents := make([]*interfaces.ScalingEvent, 0, len(mysqlEvents))
	for _, mysqlEvent := range mysqlEvents {
		domainEvents = append(domainEvents, ToScalingEventDomain(mysqlEvent))
	}
	return domainEvents
}
