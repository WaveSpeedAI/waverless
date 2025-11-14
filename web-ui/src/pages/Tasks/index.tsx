import { useState, useMemo } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  Card,
  Table,
  Button,
  Space,
  Tag,
  message,
  Popconfirm,
  Drawer,
  Typography,
  Row,
  Col,
  Descriptions,
  Statistic,
  Select,
  Collapse,
  Badge,
  Tooltip,
  Timeline,
  Tabs,
  Input,
} from 'antd';
import {
  ReloadOutlined,
  StopOutlined,
  FileTextOutlined,
  CheckCircleOutlined,
  ClockCircleOutlined,
  SyncOutlined,
  CloseCircleOutlined,
  MinusCircleOutlined,
  AppstoreOutlined,
  HistoryOutlined,
  UnorderedListOutlined,
  BranchesOutlined,
  SearchOutlined,
} from '@ant-design/icons';
import { api } from '@/api/client';
import type { Task, TaskListParams } from '@/types';

const { Title, Paragraph, Text } = Typography;

const TasksPage = () => {
  const queryClient = useQueryClient();
  const [selectedTask, setSelectedTask] = useState<Task | null>(null);
  const [detailsDrawerVisible, setDetailsDrawerVisible] = useState(false);
  const [filters, setFilters] = useState<TaskListParams>({
    limit: 20,
    offset: 0,
  });
  const [currentPage, setCurrentPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);

  // Fetch all apps to get endpoint list for filter
  const { data: apps } = useQuery({
    queryKey: ['apps'],
    queryFn: async () => {
      const response = await api.apps.list();
      return response.data;
    },
    staleTime: 30000, // Cache for 30s
  });

  // Fetch global task statistics (for overview cards)
  const { data: taskStats } = useQuery({
    queryKey: ['task-statistics'],
    queryFn: async () => {
      const response = await api.statistics.getOverview();
      return response.data;
    },
    refetchInterval: 5000, // Auto refresh every 5s
  });

  // Fetch all endpoint statistics (for endpoint queue statistics)
  const { data: endpointStatsData } = useQuery({
    queryKey: ['all-endpoint-statistics'],
    queryFn: async () => {
      const response = await api.statistics.getTopEndpoints(100); // Get all endpoints
      return response.data;
    },
    refetchInterval: 10000, // Auto refresh every 10s
  });

  // Fetch tasks - disable loading state during auto-refresh to avoid blocking UI
  const { data: tasksResponse, refetch, isFetching } = useQuery({
    queryKey: ['tasks', filters],
    queryFn: async () => {
      const response = await api.tasks.list(filters);
      return response.data;
    },
    refetchInterval: 5000, // Auto refresh every 5s
    staleTime: 0, // Always consider data stale to enable background refetch
  });

  const tasksData = tasksResponse?.tasks || [];
  const totalTasks = tasksResponse?.total || 0;

  // Cancel task mutation
  const cancelMutation = useMutation({
    mutationFn: async (taskId: string) => {
      await api.tasks.cancel(taskId);
    },
    onSuccess: () => {
      message.success('Task cancelled successfully');
      queryClient.invalidateQueries({ queryKey: ['tasks'] });
    },
    onError: (error: any) => {
      message.error(error.response?.data?.error || 'Failed to cancel task');
    },
  });

  // Fetch task events when task is selected
  const { data: eventsData } = useQuery({
    queryKey: ['task-events', selectedTask?.id],
    queryFn: async () => {
      if (!selectedTask?.id) return null;
      const response = await api.tasks.getEvents(selectedTask.id);
      return response.data;
    },
    enabled: !!selectedTask?.id && detailsDrawerVisible,
  });

  // Fetch task timeline
  const { data: timelineData } = useQuery({
    queryKey: ['task-timeline', selectedTask?.id],
    queryFn: async () => {
      if (!selectedTask?.id) return null;
      const response = await api.tasks.getTimeline(selectedTask.id);
      return response.data;
    },
    enabled: !!selectedTask?.id && detailsDrawerVisible,
  });

  // Fetch task execution history
  const { data: executionHistory } = useQuery({
    queryKey: ['task-execution-history', selectedTask?.id],
    queryFn: async () => {
      if (!selectedTask?.id) return null;
      const response = await api.tasks.getExecutionHistory(selectedTask.id);
      return response.data;
    },
    enabled: !!selectedTask?.id && detailsDrawerVisible,
  });

  const handleShowDetails = (task: Task) => {
    setSelectedTask(task);
    setDetailsDrawerVisible(true);
  };

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'COMPLETED':
        return 'success';
      case 'IN_PROGRESS':
        return 'processing';
      case 'PENDING':
        return 'default';
      case 'FAILED':
        return 'error';
      case 'CANCELLED':
        return 'warning';
      default:
        return 'default';
    }
  };

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'COMPLETED':
        return <CheckCircleOutlined />;
      case 'IN_PROGRESS':
        return <SyncOutlined spin />;
      case 'PENDING':
        return <ClockCircleOutlined />;
      case 'FAILED':
        return <CloseCircleOutlined />;
      case 'CANCELLED':
        return <MinusCircleOutlined />;
      default:
        return null;
    }
  };

  // Use global statistics from API (not calculated from current page tasks)
  const stats = useMemo(() => {
    if (taskStats) {
      return {
        total: taskStats.total || 0,
        pending: taskStats.pending || 0,
        inProgress: taskStats.in_progress || 0,
        completed: taskStats.completed || 0,
        failed: taskStats.failed || 0,
        cancelled: taskStats.cancelled || 0,
      };
    }
    // Fallback to zeros if API data not available
    return {
      total: 0,
      pending: 0,
      inProgress: 0,
      completed: 0,
      failed: 0,
      cancelled: 0,
    };
  }, [taskStats]);

  // Use per-endpoint statistics from API (not calculated from current page tasks)
  const endpointStats = useMemo(() => {
    if (endpointStatsData?.endpoints) {
      return endpointStatsData.endpoints.map(stat => ({
        endpoint: stat.endpoint,
        total: stat.total || 0,
        pending: stat.pending || 0,
        inProgress: stat.in_progress || 0,
        completed: stat.completed || 0,
        failed: stat.failed || 0,
        cancelled: stat.cancelled || 0,
      }));
    }
    return [];
  }, [endpointStatsData]);

  const columns = [
    {
      title: 'Task ID',
      dataIndex: 'id',
      key: 'id',
      render: (text: string) => (
        <Tooltip title={text}>
          <Text code>{text.substring(0, 8)}...</Text>
        </Tooltip>
      ),
      width: 100,
      fixed: 'left' as const,
    },
    {
      title: 'Endpoint',
      dataIndex: 'endpoint',
      key: 'endpoint',
      render: (text: string) => (
        <Tooltip title={text || '-'}>
          <Text strong>{text || '-'}</Text>
        </Tooltip>
      ),
      width: 150,
      ellipsis: true,
    },
    {
      title: 'Status',
      dataIndex: 'status',
      key: 'status',
      render: (status: string) => (
        <Tag icon={getStatusIcon(status)} color={getStatusColor(status)}>
          {status}
        </Tag>
      ),
      width: 140,
      filters: [
        { text: 'Pending', value: 'PENDING' },
        { text: 'In Progress', value: 'IN_PROGRESS' },
        { text: 'Completed', value: 'COMPLETED' },
        { text: 'Failed', value: 'FAILED' },
        { text: 'Cancelled', value: 'CANCELLED' },
      ],
      onFilter: (value: any, record: Task) => record.status === value,
    },
    {
      title: 'Created At',
      dataIndex: 'createdAt',
      key: 'createdAt',
      render: (date: string) => (date ? new Date(date).toLocaleString() : '-'),
      width: 180,
    },
    {
      title: 'Execution Time',
      key: 'executionTime',
      render: (_: any, record: Task) => {
        if (!record.executionTime) return '-';
        return <Text>{(record.executionTime / 1000).toFixed(2)}s</Text>;
      },
      width: 120,
    },
    {
      title: 'Worker ID',
      dataIndex: 'workerId',
      key: 'workerId',
      render: (text: string) => (
        text ? (
          <Tooltip title={text}>
            <Text code>{text.substring(0, 12)}...</Text>
          </Tooltip>
        ) : '-'
      ),
      width: 140,
      ellipsis: true,
    },
    {
      title: 'Actions',
      key: 'actions',
      width: 180,
      fixed: 'right' as const,
      render: (_: any, record: Task) => (
        <Space size="small">
          <Button
            size="small"
            icon={<FileTextOutlined />}
            onClick={() => handleShowDetails(record)}
          >
            Details
          </Button>
          {(record.status === 'PENDING' || record.status === 'IN_PROGRESS') && (
            <Popconfirm
              title="Are you sure to cancel this task?"
              onConfirm={() => cancelMutation.mutate(record.id)}
              okText="Yes"
              cancelText="No"
            >
              <Button size="small" danger icon={<StopOutlined />}>
                Cancel
              </Button>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div>
      <Row justify="space-between" align="middle" style={{ marginBottom: 12 }}>
        <Col>
          <Title level={3} style={{ margin: 0, marginBottom: 4 }}>
            <SyncOutlined /> Task Queue
          </Title>
          <Paragraph type="secondary" style={{ margin: 0, fontSize: 13 }}>
            Monitor and manage task execution status
          </Paragraph>
        </Col>
        <Col>
          <Space>
            {isFetching && (
              <Tag icon={<SyncOutlined spin />} color="processing">
                Refreshing...
              </Tag>
            )}
            <Button icon={<ReloadOutlined />} onClick={() => refetch()}>
              Refresh
            </Button>
          </Space>
        </Col>
      </Row>

      {/* Statistics Cards */}
      <Row gutter={12} style={{ marginBottom: 12 }}>
        <Col xs={24} sm={8} md={4}>
          <Card>
            <Statistic title="Total" value={stats.total} prefix={<FileTextOutlined />} />
          </Card>
        </Col>
        <Col xs={24} sm={8} md={4}>
          <Card>
            <Statistic
              title="Pending"
              value={stats.pending}
              valueStyle={{ color: '#8c8c8c' }}
              prefix={<ClockCircleOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={8} md={4}>
          <Card>
            <Statistic
              title="In Progress"
              value={stats.inProgress}
              valueStyle={{ color: '#1890ff' }}
              prefix={<SyncOutlined spin />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={8} md={4}>
          <Card>
            <Statistic
              title="Completed"
              value={stats.completed}
              valueStyle={{ color: '#52c41a' }}
              prefix={<CheckCircleOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={8} md={4}>
          <Card>
            <Statistic
              title="Failed"
              value={stats.failed}
              valueStyle={{ color: '#ff4d4f' }}
              prefix={<CloseCircleOutlined />}
            />
          </Card>
        </Col>
      </Row>

      {/* Endpoint Queue Statistics */}
      {endpointStats.length > 0 && (
        <Card
          title={
            <Space>
              <AppstoreOutlined />
              <Text strong>Endpoint Queue Statistics</Text>
              <Badge count={endpointStats.length} style={{ backgroundColor: '#52c41a' }} />
            </Space>
          }
          style={{ marginBottom: 12 }}
          size="small"
        >
          <Collapse
            items={endpointStats.map((stat) => ({
              key: stat.endpoint,
              label: (
                <Space>
                  <Text strong>{stat.endpoint}</Text>
                  <Badge count={stat.total} style={{ backgroundColor: '#1890ff' }} />
                  {stat.inProgress > 0 && (
                    <Tag icon={<SyncOutlined spin />} color="processing">
                      {stat.inProgress} running
                    </Tag>
                  )}
                  {stat.pending > 0 && (
                    <Tag icon={<ClockCircleOutlined />} color="default">
                      {stat.pending} pending
                    </Tag>
                  )}
                </Space>
              ),
              children: (
                <Row gutter={16}>
                  <Col span={4}>
                    <Statistic
                      title="Total"
                      value={stat.total}
                      valueStyle={{ fontSize: 16 }}
                    />
                  </Col>
                  <Col span={4}>
                    <Statistic
                      title="Pending"
                      value={stat.pending}
                      valueStyle={{ fontSize: 16, color: '#8c8c8c' }}
                    />
                  </Col>
                  <Col span={4}>
                    <Statistic
                      title="In Progress"
                      value={stat.inProgress}
                      valueStyle={{ fontSize: 16, color: '#1890ff' }}
                    />
                  </Col>
                  <Col span={4}>
                    <Statistic
                      title="Completed"
                      value={stat.completed}
                      valueStyle={{ fontSize: 16, color: '#52c41a' }}
                    />
                  </Col>
                  <Col span={4}>
                    <Statistic
                      title="Failed"
                      value={stat.failed}
                      valueStyle={{ fontSize: 16, color: '#ff4d4f' }}
                    />
                  </Col>
                  <Col span={4}>
                    <Statistic
                      title="Cancelled"
                      value={stat.cancelled}
                      valueStyle={{ fontSize: 16, color: '#faad14' }}
                    />
                  </Col>
                </Row>
              ),
            }))}
          />
        </Card>
      )}

      {/* Filters */}
      <Card style={{ marginBottom: 12 }} size="small">
        <Space wrap>
          <Text strong>Filters:</Text>
          <Input.Search
            placeholder="Search by Task ID"
            allowClear
            style={{ width: 250 }}
            prefix={<SearchOutlined />}
            onSearch={(value) => {
              setFilters({ ...filters, task_id: value || undefined, offset: 0 });
              setCurrentPage(1);
            }}
          />
          <Select
            placeholder="Filter by endpoint"
            allowClear
            showSearch
            style={{ width: 250 }}
            onChange={(value) => {
              setFilters({ ...filters, endpoint: value, offset: 0 });
              setCurrentPage(1);
            }}
            options={apps?.map((app) => ({
              label: app.name,
              value: app.name,
            })) || []}
          />
          <Select
            placeholder="Filter by status"
            allowClear
            style={{ width: 150 }}
            onChange={(value) => {
              setFilters({ ...filters, status: value, offset: 0 });
              setCurrentPage(1);
            }}
            options={[
              { label: 'Pending', value: 'PENDING' },
              { label: 'In Progress', value: 'IN_PROGRESS' },
              { label: 'Completed', value: 'COMPLETED' },
              { label: 'Failed', value: 'FAILED' },
              { label: 'Cancelled', value: 'CANCELLED' },
            ]}
          />
        </Space>
      </Card>

      {/* Tasks Table */}
      <Card>
        <Table
          columns={columns}
          dataSource={tasksData}
          rowKey="id"
          loading={false}
          scroll={{ x: 1100 }}
          pagination={{
            current: currentPage,
            pageSize: pageSize,
            showSizeChanger: true,
            pageSizeOptions: ['10', '20', '50', '100'],
            showTotal: (total, range) => `${range[0]}-${range[1]} of ${total} tasks`,
            total: totalTasks,
            onChange: (page, newPageSize) => {
              const newOffset = (page - 1) * (newPageSize || pageSize);
              setCurrentPage(page);
              if (newPageSize !== pageSize) {
                setPageSize(newPageSize || pageSize);
                setFilters({ ...filters, limit: newPageSize || pageSize, offset: newOffset });
              } else {
                setFilters({ ...filters, offset: newOffset });
              }
            },
            onShowSizeChange: (_current, size) => {
              setPageSize(size);
              setCurrentPage(1);
              setFilters({ ...filters, limit: size, offset: 0 });
            },
          }}
        />
      </Card>

      {/* Task Details Drawer */}
      <Drawer
        title={<Space><FileTextOutlined /> Task Details</Space>}
        placement="right"
        onClose={() => setDetailsDrawerVisible(false)}
        open={detailsDrawerVisible}
        width="70%"
      >
        {selectedTask && (
          <Tabs
            defaultActiveKey="details"
            items={[
              {
                key: 'details',
                label: <Space><FileTextOutlined /> Details</Space>,
                children: (
                  <div>
                    <Descriptions column={1} bordered>
                      <Descriptions.Item label="Task ID">
                        <Text code>{selectedTask.id}</Text>
                      </Descriptions.Item>
                      {selectedTask.endpoint && (
                        <Descriptions.Item label="Endpoint">
                          <Text strong>{selectedTask.endpoint}</Text>
                        </Descriptions.Item>
                      )}
                      <Descriptions.Item label="Status">
                        <Tag icon={getStatusIcon(selectedTask.status)} color={getStatusColor(selectedTask.status)}>
                          {selectedTask.status}
                        </Tag>
                      </Descriptions.Item>
                      {selectedTask.workerId && (
                        <Descriptions.Item label="Worker ID">
                          <Text code>{selectedTask.workerId}</Text>
                        </Descriptions.Item>
                      )}
                      {selectedTask.createdAt && (
                        <Descriptions.Item label="Created At">
                          {new Date(selectedTask.createdAt).toLocaleString()}
                        </Descriptions.Item>
                      )}
                      {selectedTask.delayTime !== undefined && (
                        <Descriptions.Item label="Delay Time">
                          {selectedTask.delayTime}ms
                        </Descriptions.Item>
                      )}
                      {selectedTask.executionTime !== undefined && (
                        <Descriptions.Item label="Execution Time">
                          {(selectedTask.executionTime / 1000).toFixed(2)}s
                        </Descriptions.Item>
                      )}
                    </Descriptions>

                    {selectedTask.input && Object.keys(selectedTask.input).length > 0 && (
                      <Card title="Input" style={{ marginTop: 16 }} size="small">
                        <pre style={{ maxHeight: 300, overflow: 'auto', fontSize: 12 }}>
                          {JSON.stringify(selectedTask.input, null, 2)}
                        </pre>
                      </Card>
                    )}

                    {selectedTask.output && Object.keys(selectedTask.output).length > 0 && (
                      <Card title="Output" style={{ marginTop: 16 }} size="small">
                        <pre style={{ maxHeight: 300, overflow: 'auto', fontSize: 12 }}>
                          {JSON.stringify(selectedTask.output, null, 2)}
                        </pre>
                      </Card>
                    )}

                    {selectedTask.error && (
                      <Card title="Error" style={{ marginTop: 16 }} size="small">
                        <Text type="danger" style={{ fontFamily: 'monospace', fontSize: 12 }}>
                          {selectedTask.error}
                        </Text>
                      </Card>
                    )}
                  </div>
                ),
              },
              {
                key: 'timeline',
                label: <Space><BranchesOutlined /> Timeline</Space>,
                children: (
                  <Card>
                    {timelineData && timelineData.timeline && timelineData.timeline.length > 0 ? (
                      <Timeline
                        items={timelineData.timeline.map((event) => ({
                          color: event.event_type.includes('FAILED') ? 'red' :
                                 event.event_type.includes('COMPLETED') ? 'green' :
                                 event.event_type.includes('TIMEOUT') ? 'orange' :
                                 event.event_type.includes('ORPHANED') ? 'volcano' : 'blue',
                          children: (
                            <div>
                              <Text strong>{event.event_type}</Text>
                              <br />
                              <Text type="secondary" style={{ fontSize: 12 }}>
                                {new Date(event.event_time).toLocaleString()}
                              </Text>
                              {event.worker_id && (
                                <>
                                  <br />
                                  <Tooltip title={event.worker_id}>
                                    <Text code style={{ fontSize: 11 }}>Worker: {event.worker_id.substring(0, 12)}...</Text>
                                  </Tooltip>
                                </>
                              )}
                              {event.error_message && (
                                <>
                                  <br />
                                  <Text type="danger" style={{ fontSize: 12 }}>{event.error_message}</Text>
                                </>
                              )}
                            </div>
                          ),
                        }))}
                      />
                    ) : (
                      <Text type="secondary">No timeline events available</Text>
                    )}
                  </Card>
                ),
              },
              {
                key: 'events',
                label: <Space><UnorderedListOutlined /> All Events ({eventsData?.total || 0})</Space>,
                children: (
                  <Card>
                    {eventsData && eventsData.events && eventsData.events.length > 0 ? (
                      <Table
                        dataSource={eventsData.events}
                        rowKey="id"
                        size="small"
                        pagination={{ pageSize: 10 }}
                        columns={[
                          {
                            title: 'Event Type',
                            dataIndex: 'event_type',
                            key: 'event_type',
                            render: (text) => <Tag>{text}</Tag>,
                          },
                          {
                            title: 'Time',
                            dataIndex: 'event_time',
                            key: 'event_time',
                            render: (text) => new Date(text).toLocaleString(),
                          },
                          {
                            title: 'Worker ID',
                            dataIndex: 'worker_id',
                            key: 'worker_id',
                            render: (text) => text ? (
                              <Tooltip title={text}>
                                <Text code>{text.substring(0, 12)}...</Text>
                              </Tooltip>
                            ) : '-',
                            ellipsis: true,
                          },
                          {
                            title: 'Status',
                            key: 'status',
                            render: (_, record) => (
                              <Space size="small">
                                {record.from_status && <Tag color="default">{record.from_status}</Tag>}
                                {record.from_status && record.to_status && '→'}
                                {record.to_status && <Tag color="processing">{record.to_status}</Tag>}
                              </Space>
                            ),
                          },
                          {
                            title: 'Error',
                            dataIndex: 'error_message',
                            key: 'error_message',
                            render: (text) => text ? (
                              <Tooltip title={text}>
                                <Text type="danger" ellipsis style={{ maxWidth: 200 }}>{text}</Text>
                              </Tooltip>
                            ) : '-',
                            ellipsis: true,
                          },
                        ]}
                      />
                    ) : (
                      <Text type="secondary">No events available</Text>
                    )}
                  </Card>
                ),
              },
              {
                key: 'execution',
                label: <Space><HistoryOutlined /> Execution History</Space>,
                children: (
                  <Card>
                    {executionHistory && executionHistory.history && executionHistory.history.length > 0 ? (
                      <Table
                        dataSource={executionHistory.history}
                        rowKey={(record, index) => `${record.worker_id}-${index}`}
                        size="small"
                        pagination={false}
                        columns={[
                          {
                            title: 'Worker ID',
                            dataIndex: 'worker_id',
                            key: 'worker_id',
                            render: (text) => (
                              <Tooltip title={text}>
                                <Text code>{text}</Text>
                              </Tooltip>
                            ),
                            ellipsis: true,
                          },
                          {
                            title: 'Start Time',
                            dataIndex: 'start_time',
                            key: 'start_time',
                            render: (text) => new Date(text).toLocaleString(),
                          },
                          {
                            title: 'End Time',
                            dataIndex: 'end_time',
                            key: 'end_time',
                            render: (text) => text ? new Date(text).toLocaleString() : <Text type="secondary">In Progress</Text>,
                          },
                          {
                            title: 'Duration',
                            dataIndex: 'duration_seconds',
                            key: 'duration_seconds',
                            render: (seconds) => seconds !== undefined ? `${seconds.toFixed(2)}s` : '-',
                          },
                        ]}
                      />
                    ) : (
                      <Text type="secondary">No execution history available</Text>
                    )}
                  </Card>
                ),
              },
            ]}
          />
        )}
      </Drawer>
    </div>
  );
};

export default TasksPage;
