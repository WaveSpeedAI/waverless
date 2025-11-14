import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  Card,
  Row,
  Col,
  Statistic,
  Tag,
  Space,
  Typography,
  Progress,
  List,
  Badge,
  Tooltip,
} from 'antd';
import {
  AppstoreOutlined,
  CheckCircleOutlined,
  ClockCircleOutlined,
  SyncOutlined,
  CloseCircleOutlined,
  RocketOutlined,
  FileTextOutlined,
  DashboardOutlined,
  MinusCircleOutlined,
} from '@ant-design/icons';
import { api } from '@/api/client';
import type { AppInfo, Task } from '@/types';
import { useNavigate } from 'react-router-dom';

const { Title, Paragraph, Text } = Typography;

const DashboardPage = () => {
  const navigate = useNavigate();

  // Fetch apps
  const { data: apps } = useQuery({
    queryKey: ['apps'],
    queryFn: async () => {
      const response = await api.apps.list();
      return response.data;
    },
    refetchInterval: 10000, // Auto refresh every 10s
  });

  // Fetch global task statistics (replaces tasks.list)
  const { data: taskStats } = useQuery({
    queryKey: ['task-statistics'],
    queryFn: async () => {
      const response = await api.statistics.getOverview();
      return response.data;
    },
    refetchInterval: 5000, // Auto refresh every 5s
  });

  // Fetch top endpoint statistics
  const { data: endpointStatsData } = useQuery({
    queryKey: ['endpoint-statistics'],
    queryFn: async () => {
      const response = await api.statistics.getTopEndpoints(5);
      return response.data;
    },
    refetchInterval: 10000, // Auto refresh every 10s
  });

  // Fetch recent tasks for display only (limit to 10)
  const { data: tasksResponse } = useQuery({
    queryKey: ['recent-tasks'],
    queryFn: async () => {
      const response = await api.tasks.list({ limit: 10 });
      return response.data;
    },
    refetchInterval: 5000, // Auto refresh every 5s
  });

  const tasks = Array.isArray(tasksResponse?.tasks) ? tasksResponse.tasks : [];

  // Calculate app statistics
  const appStats = useMemo(() => {
    if (!apps) return { total: 0, running: 0, pending: 0, failed: 0, totalReplicas: 0, readyReplicas: 0 };

    return {
      total: apps.length,
      running: apps.filter(app => app.status === 'Running').length,
      pending: apps.filter(app => app.status === 'Pending').length,
      failed: apps.filter(app => app.status === 'Failed' || app.status === 'Error').length,
      totalReplicas: apps.reduce((sum, app) => sum + (app.replicas || 0), 0),
      readyReplicas: apps.reduce((sum, app) => sum + (app.readyReplicas || 0), 0),
    };
  }, [apps]);

  // Use statistics from API instead of calculating from limited task list
  const taskStatsCalculated = useMemo(() => {
    // If statistics API data is available, use it (this is the full count from database)
    if (taskStats) {
      return {
        total: taskStats.total,
        pending: taskStats.pending,
        inProgress: taskStats.in_progress,
        completed: taskStats.completed,
        failed: taskStats.failed,
        cancelled: taskStats.cancelled,
      };
    }
    // Fallback: calculate from limited task list (not accurate for large datasets)
    if (!tasks.length) return { total: 0, pending: 0, inProgress: 0, completed: 0, failed: 0, cancelled: 0 };
    return {
      total: tasks.length,
      pending: tasks.filter(t => t.status === 'PENDING').length,
      inProgress: tasks.filter(t => t.status === 'IN_PROGRESS').length,
      completed: tasks.filter(t => t.status === 'COMPLETED').length,
      failed: tasks.filter(t => t.status === 'FAILED').length,
      cancelled: tasks.filter(t => t.status === 'CANCELLED').length,
    };
  }, [taskStats, tasks]);

  // Calculate health percentage
  const healthPercentage = appStats.total > 0
    ? Math.round((appStats.running / appStats.total) * 100)
    : 0;

  // Calculate replica health percentage
  const replicaHealthPercentage = appStats.totalReplicas > 0
    ? Math.round((appStats.readyReplicas / appStats.totalReplicas) * 100)
    : 0;

  // Get recent apps (sorted by creation time)
  const recentApps = useMemo(() => {
    if (!apps) return [];
    return [...apps]
      .sort((a, b) => new Date(b.createdAt || 0).getTime() - new Date(a.createdAt || 0).getTime())
      .slice(0, 5);
  }, [apps]);

  // Get recent tasks (sorted by creation time)
  const recentTasks = useMemo(() => {
    if (!tasks.length) return [];
    return [...tasks]
      .sort((a, b) => new Date(b.createdAt || 0).getTime() - new Date(a.createdAt || 0).getTime())
      .slice(0, 10);
  }, [tasks]);

  // Per-endpoint task statistics from API (more accurate than calculating from limited task list)
  const endpointTaskStats = useMemo(() => {
    // Use statistics from API if available
    if (endpointStatsData?.endpoints) {
      return endpointStatsData.endpoints.map(stat => ({
        endpoint: stat.endpoint,
        total: stat.total,
        pending: stat.pending,
        inProgress: stat.in_progress,
        completed: stat.completed,
        failed: stat.failed,
      }));
    }

    // Fallback: calculate from limited task list (not accurate)
    if (!tasks.length) return [];

    const statsMap = new Map<string, {
      endpoint: string;
      total: number;
      pending: number;
      inProgress: number;
      completed: number;
      failed: number;
    }>();

    tasks.forEach((task) => {
      const endpoint = task.endpoint || 'unknown';
      if (!statsMap.has(endpoint)) {
        statsMap.set(endpoint, {
          endpoint,
          total: 0,
          pending: 0,
          inProgress: 0,
          completed: 0,
          failed: 0,
        });
      }

      const stat = statsMap.get(endpoint)!;
      stat.total++;

      switch (task.status) {
        case 'PENDING':
          stat.pending++;
          break;
        case 'IN_PROGRESS':
          stat.inProgress++;
          break;
        case 'COMPLETED':
          stat.completed++;
          break;
        case 'FAILED':
          stat.failed++;
          break;
      }
    });

    return Array.from(statsMap.values()).sort((a, b) => b.total - a.total).slice(0, 5);
  }, [endpointStatsData, tasks]);

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'Running':
        return 'green';
      case 'Pending':
        return 'orange';
      case 'Failed':
      case 'Error':
        return 'red';
      default:
        return 'default';
    }
  };

  const getTaskStatusColor = (status: string) => {
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

  const getTaskStatusIcon = (status: string) => {
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

  return (
    <div>
      <Row justify="space-between" align="middle" style={{ marginBottom: 12 }}>
        <Col>
          <Title level={3} style={{ margin: 0, marginBottom: 4 }}>
            <DashboardOutlined /> Dashboard
          </Title>
          <Paragraph type="secondary" style={{ margin: 0, fontSize: 13 }}>
            System overview and monitoring
          </Paragraph>
        </Col>
      </Row>

      {/* Key Metrics */}
      <Row gutter={12} style={{ marginBottom: 12 }}>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="Total Applications"
              value={appStats.total}
              prefix={<AppstoreOutlined />}
              valueStyle={{ color: '#1890ff' }}
            />
            <Progress
              percent={healthPercentage}
              size="small"
              status={healthPercentage >= 80 ? 'success' : healthPercentage >= 50 ? 'normal' : 'exception'}
              format={() => `${appStats.running} running`}
              style={{ marginTop: 8 }}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="Total Tasks"
              value={taskStatsCalculated.total}
              prefix={<FileTextOutlined />}
              valueStyle={{ color: '#52c41a' }}
            />
            <Space size="small" style={{ marginTop: 8 }}>
              <Tag color="processing">{taskStatsCalculated.inProgress} running</Tag>
              <Tag color="default">{taskStatsCalculated.pending} pending</Tag>
            </Space>
          </Card>
        </Col>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="Replica Health"
              value={replicaHealthPercentage}
              suffix="%"
              prefix={<CheckCircleOutlined />}
              valueStyle={{
                color: replicaHealthPercentage >= 80 ? '#52c41a' : replicaHealthPercentage >= 50 ? '#faad14' : '#ff4d4f'
              }}
            />
            <Text type="secondary" style={{ fontSize: 12 }}>
              {appStats.readyReplicas} / {appStats.totalReplicas} replicas ready
            </Text>
          </Card>
        </Col>
        <Col xs={24} sm={12} md={6}>
          <Card>
            <Statistic
              title="Task Success Rate"
              value={
                taskStatsCalculated.total > 0
                  ? Math.round((taskStatsCalculated.completed / taskStatsCalculated.total) * 100)
                  : 0
              }
              suffix="%"
              prefix={<RocketOutlined />}
              valueStyle={{ color: '#722ed1' }}
            />
            <Space size="small" style={{ marginTop: 8 }}>
              <Tag color="success">{taskStatsCalculated.completed} completed</Tag>
              <Tag color="error">{taskStatsCalculated.failed} failed</Tag>
            </Space>
          </Card>
        </Col>
      </Row>

      {/* Application Status Overview */}
      <Row gutter={12} style={{ marginBottom: 12 }}>
        <Col xs={24} lg={12}>
          <Card
            size="small"
            title={
              <Space>
                <AppstoreOutlined />
                <Text strong>Application Status</Text>
              </Space>
            }
          >
            <Row gutter={16}>
              <Col span={8}>
                <Statistic
                  title="Running"
                  value={appStats.running}
                  valueStyle={{ fontSize: 24, color: '#52c41a' }}
                  prefix={<CheckCircleOutlined />}
                />
              </Col>
              <Col span={8}>
                <Statistic
                  title="Pending"
                  value={appStats.pending}
                  valueStyle={{ fontSize: 24, color: '#faad14' }}
                  prefix={<ClockCircleOutlined />}
                />
              </Col>
              <Col span={8}>
                <Statistic
                  title="Failed"
                  value={appStats.failed}
                  valueStyle={{ fontSize: 24, color: '#ff4d4f' }}
                  prefix={<CloseCircleOutlined />}
                />
              </Col>
            </Row>
          </Card>
        </Col>

        <Col xs={24} lg={12}>
          <Card
            size="small"
            title={
              <Space>
                <FileTextOutlined />
                <Text strong>Task Queue Status</Text>
              </Space>
            }
          >
            <Row gutter={16}>
              <Col span={8}>
                <Statistic
                  title="In Progress"
                  value={taskStatsCalculated.inProgress}
                  valueStyle={{ fontSize: 24, color: '#1890ff' }}
                  prefix={<SyncOutlined spin />}
                />
              </Col>
              <Col span={8}>
                <Statistic
                  title="Pending"
                  value={taskStatsCalculated.pending}
                  valueStyle={{ fontSize: 24, color: '#8c8c8c' }}
                  prefix={<ClockCircleOutlined />}
                />
              </Col>
              <Col span={8}>
                <Statistic
                  title="Completed"
                  value={taskStatsCalculated.completed}
                  valueStyle={{ fontSize: 24, color: '#52c41a' }}
                  prefix={<CheckCircleOutlined />}
                />
              </Col>
            </Row>
          </Card>
        </Col>
      </Row>

      {/* Top Endpoints by Task Volume */}
      {endpointTaskStats.length > 0 && (
        <Row gutter={12} style={{ marginBottom: 12 }}>
          <Col xs={24}>
            <Card
              size="small"
              title={
                <Space>
                  <RocketOutlined />
                  <Text strong>Top Endpoints by Task Volume</Text>
                </Space>
              }
            >
              <List
                dataSource={endpointTaskStats}
                renderItem={(stat) => (
                  <List.Item>
                    <Space direction="vertical" style={{ width: '100%' }}>
                      <Space style={{ width: '100%', justifyContent: 'space-between' }}>
                        <Space>
                          <Text strong>{stat.endpoint}</Text>
                          <Badge count={stat.total} style={{ backgroundColor: '#1890ff' }} />
                        </Space>
                        <Space size="small">
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
                          {stat.failed > 0 && (
                            <Tag icon={<CloseCircleOutlined />} color="error">
                              {stat.failed} failed
                            </Tag>
                          )}
                        </Space>
                      </Space>
                      <Progress
                        percent={stat.total > 0 ? Math.round((stat.completed / stat.total) * 100) : 0}
                        size="small"
                        status="active"
                        format={() => `${stat.completed} completed`}
                      />
                    </Space>
                  </List.Item>
                )}
              />
            </Card>
          </Col>
        </Row>
      )}

      {/* Recent Activity */}
      <Row gutter={12}>
        <Col xs={24} lg={12}>
          <Card
            size="small"
            title={
              <Space>
                <AppstoreOutlined />
                <Text strong>Recent Applications</Text>
              </Space>
            }
            extra={
              <a onClick={() => navigate('/apps')} style={{ fontSize: 12 }}>
                View All →
              </a>
            }
          >
            <List
              dataSource={recentApps}
              renderItem={(app: AppInfo) => (
                <List.Item>
                  <List.Item.Meta
                    title={
                      <Space>
                        <Text strong style={{ fontSize: 13 }}>{app.name}</Text>
                        <Tag color={getStatusColor(app.status)} style={{ fontSize: 11 }}>
                          {app.status}
                        </Tag>
                      </Space>
                    }
                    description={
                      <Space direction="vertical" size={0} style={{ width: '100%' }}>
                        <Tooltip title={app.image}>
                          <Text type="secondary" style={{ fontSize: 11 }} ellipsis>
                            {app.image}
                          </Text>
                        </Tooltip>
                        <Text type="secondary" style={{ fontSize: 11 }}>
                          Replicas: {app.readyReplicas || 0}/{app.replicas || 0}
                        </Text>
                      </Space>
                    }
                  />
                </List.Item>
              )}
            />
          </Card>
        </Col>

        <Col xs={24} lg={12}>
          <Card
            size="small"
            title={
              <Space>
                <FileTextOutlined />
                <Text strong>Recent Tasks</Text>
              </Space>
            }
            extra={
              <a onClick={() => navigate('/tasks')} style={{ fontSize: 12 }}>
                View All →
              </a>
            }
          >
            <List
              dataSource={recentTasks}
              renderItem={(task: Task) => (
                <List.Item>
                  <List.Item.Meta
                    title={
                      <Space>
                        <Tooltip title={task.id}>
                          <Text code style={{ fontSize: 11 }}>{task.id.substring(0, 8)}...</Text>
                        </Tooltip>
                        <Tag
                          icon={getTaskStatusIcon(task.status)}
                          color={getTaskStatusColor(task.status)}
                          style={{ fontSize: 11 }}
                        >
                          {task.status}
                        </Tag>
                      </Space>
                    }
                    description={
                      <Space size="small">
                        <Text type="secondary" style={{ fontSize: 11 }}>
                          {task.endpoint || 'unknown'}
                        </Text>
                        <Text type="secondary" style={{ fontSize: 11 }}>
                          {task.createdAt ? new Date(task.createdAt).toLocaleTimeString() : '-'}
                        </Text>
                      </Space>
                    }
                  />
                </List.Item>
              )}
            />
          </Card>
        </Col>
      </Row>
    </div>
  );
};

export default DashboardPage;
