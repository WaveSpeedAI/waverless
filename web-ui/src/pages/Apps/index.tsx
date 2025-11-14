import { useEffect, useMemo, useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Badge,
  Button,
  Card,
  Col,
  Descriptions,
  Divider,
  Dropdown,
  Drawer,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Progress,
  Row,
  Select,
  Space,
  Statistic,
  Switch,
  Table,
  Tabs,
  Tag,
  Timeline,
  Tooltip,
  Typography,
  message,
} from 'antd';
import type { MenuProps } from 'antd';
import {
  ReloadOutlined,
  DeleteOutlined,
  EditOutlined,
  SyncOutlined,
  ClockCircleOutlined,
  ThunderboltOutlined,
  CloudOutlined,
  HistoryOutlined,
  SettingOutlined,
  RiseOutlined,
  FallOutlined,
  EllipsisOutlined,
  FileTextOutlined,
  QuestionCircleOutlined,
  UserOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  LoadingOutlined,
  CodeOutlined,
  GlobalOutlined,
  BranchesOutlined,
  UnorderedListOutlined,
} from '@ant-design/icons';
import { api } from '@/api/client';
import type {
  AppInfo,
  UpdateDeploymentRequest,
  UpdateEndpointConfigRequest,
  Task,
  SpecInfo,
  Worker,
  WorkerWithPodInfo,
} from '@/types';
import Terminal from '@/components/Terminal';

const { Title, Text, Paragraph } = Typography;
const { TextArea } = Input;

// AutoScaler related types
interface ClusterResources {
  total: { gpuCount: number; cpuCores: number; memoryGB: number };
  used: { gpuCount: number; cpuCores: number; memoryGB: number };
  available: { gpuCount: number; cpuCores: number; memoryGB: number };
}

interface AutoScalerClusterStatus {
  enabled: boolean;
  running: boolean;
  lastRunTime: string;
  clusterResources: ClusterResources;
}

interface ScalingEvent {
  id: string;
  endpoint: string;
  timestamp: string;
  action: string;
  fromReplicas: number;
  toReplicas: number;
  reason: string;
  queueLength: number;
  priority?: number;
}

interface GlobalConfig {
  enabled?: boolean;
  interval: number;
  maxGpuCount: number;
  maxCpuCores: number;
  maxMemoryGB: number;
  starvationTime: number;
}

const GLOBAL_TASK_TIMEOUT = 3600;

const AUTOSCALER_FIELD_TIPS = {
  priority: 'Higher numbers get resources first. Default 50.',
  scaleUpThreshold: 'Pending tasks required before adding replicas (>=1).',
  scaleDownIdleTime: 'Idle seconds before replicas are removed. Larger = fewer flaps.',
  scaleUpCooldown: 'Minimum seconds between two scale-up actions.',
  scaleDownCooldown: 'Minimum seconds between two scale-down actions.',
  highLoadThreshold: 'Queue length treated as "high load" for dynamic priority.',
  priorityBoost: 'How many priority points to add when high load is detected.',
} as const;

type AutoScalerTipKey = keyof typeof AUTOSCALER_FIELD_TIPS;

const renderFieldLabel = (label: string, key: AutoScalerTipKey) => (
  <span>
    {label}
    <Tooltip title={AUTOSCALER_FIELD_TIPS[key]}>
      <QuestionCircleOutlined style={{ marginLeft: 4, color: '#999' }} />
    </Tooltip>
  </span>
);

const renderZeroHint = (condition: boolean, text: string) =>
  condition ? (
    <Text type="secondary" style={{ marginLeft: 8 }}>
      {text}
    </Text>
  ) : null;

const formatTaskTimeout = (value?: number) =>
  value && value > 0 ? `${value}` : `0 (uses global default ${GLOBAL_TASK_TIMEOUT}s)`;

const AppsPage = () => {
  const queryClient = useQueryClient();
  const [selectedApp, setSelectedApp] = useState<string>('');
  const [updateModalVisible, setUpdateModalVisible] = useState(false);
  const [metadataModalVisible, setMetadataModalVisible] = useState(false);
  const [globalConfigModalVisible, setGlobalConfigModalVisible] = useState(false);
  const [detailsDrawerVisible, setDetailsDrawerVisible] = useState(false);
  const [isManualRefreshing, setIsManualRefreshing] = useState(false);
  const [expandedRowKeys, setExpandedRowKeys] = useState<string[]>([]);
  const [updateForm] = Form.useForm();
  const [metadataForm] = Form.useForm();
  const [globalConfigForm] = Form.useForm();

  // Fetch apps - disable loading state during auto-refresh to avoid blocking UI
  const { data: apps, refetch } = useQuery({
    queryKey: ['apps'],
    queryFn: async () => {
      const response = await api.apps.list();
      return response.data;
    },
    refetchInterval: 10000, // Auto refresh every 10s
    staleTime: 0, // Always consider data stale to enable background refetch
  });

  // Fetch cluster resources (lightweight endpoint) - only when panel is visible
  const { data: clusterStatus } = useQuery<AutoScalerClusterStatus>({
    queryKey: ['cluster-resources'],
    queryFn: async () => {
      const response = await api.autoscaler.getClusterResources();
      return response.data;
    },
    refetchInterval: 15000, // Refresh every 15s
  });

  // Fetch recent events (lightweight endpoint) - only when panel is visible
  const { data: recentEvents } = useQuery<ScalingEvent[]>({
    queryKey: ['recent-events'],
    queryFn: async () => {
      const response = await api.autoscaler.getRecentEvents(10);
      return response.data;
    },
    refetchInterval: 20000, // Refresh every 20s
  });

  // Fetch global config
  const { data: globalConfig } = useQuery({
    queryKey: ['autoscaler-config'],
    queryFn: async () => {
      const response = await api.autoscaler.getGlobalConfig();
      return response.data;
    },
  });

  useEffect(() => {
    if (globalConfig) {
      globalConfigForm.setFieldsValue(globalConfig);
    }
  }, [globalConfig, globalConfigForm]);

  // Fetch specs for update form
  const { data: specs } = useQuery<SpecInfo[]>({
    queryKey: ['specs'],
    queryFn: async () => {
      const response = await api.specs.list();
      return response.data;
    },
  });

  // Fetch app details
  const { data: appDetails } = useQuery({
    queryKey: ['app', selectedApp],
    queryFn: async () => {
      const response = await api.apps.get(selectedApp);
      return response.data;
    },
    enabled: !!selectedApp && (metadataModalVisible || detailsDrawerVisible),
  });

  // Enable/Disable autoscaler mutations
  const toggleAutoscalerMutation = useMutation({
    mutationFn: async (enabled: boolean) => {
      if (enabled) {
        await api.autoscaler.enable();
      } else {
        await api.autoscaler.disable();
      }
    },
    onMutate: async (enabled: boolean) => {
      await queryClient.cancelQueries({ queryKey: ['cluster-resources'] });
      const previousStatus = queryClient.getQueryData<AutoScalerClusterStatus>(['cluster-resources']);
      queryClient.setQueryData<AutoScalerClusterStatus | undefined>(['cluster-resources'], (old) =>
        old ? { ...old, enabled } : old
      );
      return { previousStatus };
    },
    onSuccess: (_, enabled) => {
      message.success(`AutoScaler ${enabled ? 'enabled' : 'disabled'} successfully`);
      queryClient.invalidateQueries({ queryKey: ['cluster-resources'] });
      queryClient.invalidateQueries({ queryKey: ['recent-events'] });
    },
    onError: (error: any, _enabled, context) => {
      if (context?.previousStatus) {
        queryClient.setQueryData(['cluster-resources'], context.previousStatus);
      }
      message.error(error.response?.data?.error || 'Failed to toggle autoscaler');
    },
  });

  const triggerAutoscalerMutation = useMutation({
    mutationFn: async (endpoint?: string) => {
      await api.autoscaler.trigger(endpoint);
    },
    onSuccess: (_, endpoint) => {
      message.success(endpoint ? `AutoScaler triggered for ${endpoint}` : 'AutoScaler triggered for all endpoints');
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      queryClient.invalidateQueries({ queryKey: ['cluster-resources'] });
      queryClient.invalidateQueries({ queryKey: ['recent-events'] });
    },
    onError: (error: any) => {
      message.error(error.response?.data?.error || 'Failed to trigger autoscaler');
    },
  });

  const updateGlobalConfigMutation = useMutation({
    mutationFn: async (config: GlobalConfig) => {
      // Ensure to preserve current enabled status, avoid accidental override
      const configToSubmit = {
        ...config,
        enabled: globalConfig?.enabled ?? true, // Preserve current state
      };
      await api.autoscaler.updateGlobalConfig(configToSubmit);
      return configToSubmit;
    },
    onSuccess: (config) => {
      message.success('Global AutoScaler configuration updated');
      setGlobalConfigModalVisible(false);
      queryClient.setQueryData(['autoscaler-config'], (old: any) => ({
        ...(old || {}),
        ...config,
      }));
      queryClient.invalidateQueries({ queryKey: ['autoscaler-config'] });
      queryClient.invalidateQueries({ queryKey: ['cluster-resources'] });
      queryClient.invalidateQueries({ queryKey: ['recent-events'] });
      queryClient.invalidateQueries({ queryKey: ['apps'] });
    },
    onError: (error: any) => {
      message.error(error.response?.data?.error || 'Failed to update global configuration');
    },
  });

  // Delete mutation
  const deleteMutation = useMutation({
    mutationFn: async (name: string) => {
      await api.apps.delete(name);
    },
    onSuccess: (_resp, name) => {
      message.success('Application deleted successfully');
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      queryClient.setQueryData<AppInfo[] | undefined>(['apps'], (old) =>
        old ? old.filter((app) => app.name !== name) : old
      );
    },
    onError: (error: any) => {
      message.error(error.response?.data?.error || 'Failed to delete application');
    },
  });

  // Update mutation
  const updateMutation = useMutation({
    mutationFn: async (data: { name: string; request: UpdateDeploymentRequest }) => {
      const response = await api.apps.update(data.name, data.request);
      return response.data;
    },
    onSuccess: (_resp, variables) => {
      message.success('Deployment updated successfully');
      setUpdateModalVisible(false);
      updateForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      queryClient.setQueryData<AppInfo[] | undefined>(['apps'], (old) =>
        old
          ? old.map((app) =>
              app.name === variables.name
                ? {
                    ...app,
                    image: variables.request.image ?? app.image,
                    specName: variables.request.specName ?? app.specName,
                    replicas: variables.request.replicas ?? app.replicas,
                  }
                : app
            )
          : old
      );
      queryClient.setQueryData<AppInfo | undefined>(['app', variables.name], (old) =>
        old
          ? {
              ...old,
              image: variables.request.image ?? old.image,
              specName: variables.request.specName ?? old.specName,
              replicas: variables.request.replicas ?? old.replicas,
            }
          : old
      );
    },
    onError: (error: any) => {
      message.error(error.response?.data?.error || 'Failed to update deployment');
    },
  });

  // Update metadata mutation
  const updateMetadataMutation = useMutation({
    mutationFn: async (data: { name: string; config: UpdateEndpointConfigRequest }) => {
      const response = await api.apps.updateMetadata(data.name, data.config);
      return response.data;
    },
    onSuccess: (_resp, variables) => {
      message.success('Configuration updated successfully');
      setMetadataModalVisible(false);
      metadataForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      queryClient.invalidateQueries({ queryKey: ['app', variables.name] });
    },
    onError: (error: any) => {
      message.error(error.response?.data?.error || 'Failed to update configuration');
    },
  });

  const handleManualRefresh = async () => {
    setIsManualRefreshing(true);
    try {
      const result = await refetch();
      if (result.error) {
        throw result.error;
      }
      message.success('Applications refreshed');
    } catch (error: any) {
      message.error(error?.message || 'Failed to refresh applications');
    } finally {
      setIsManualRefreshing(false);
    }
  };

  const handleUpdate = (values: any) => {
    const request: UpdateDeploymentRequest = {
      endpoint: selectedApp,
    };
    if (values.specName) request.specName = values.specName;
    if (values.image) request.image = values.image;
    if (values.replicas !== undefined && values.replicas !== null) {
      request.replicas = values.replicas;
    }

    updateMutation.mutate({ name: selectedApp, request });
  };

  const handleEditMetadata = (app?: AppInfo) => {
    const target = app || appDetails;
    if (!target) return;

    setSelectedApp(target.name);
    metadataForm.setFieldsValue({
      displayName: target.displayName || target.name,
      description: target.description || '',
      minReplicas: target.minReplicas ?? 0,
      maxReplicas: target.maxReplicas ?? 1,  // Default to 1, not 10
      priority: target.priority ?? 50,
      taskTimeout: target.taskTimeout ?? 0,
      autoscalerEnabled: target.autoscalerEnabled || '',
      scaleUpThreshold: target.scaleUpThreshold ?? 1,
      scaleDownIdleTime: target.scaleDownIdleTime ?? 300,
      scaleUpCooldown: target.scaleUpCooldown ?? 30,
      scaleDownCooldown: target.scaleDownCooldown ?? 60,
      enableDynamicPrio: target.enableDynamicPrio ?? true,
      highLoadThreshold: target.highLoadThreshold ?? 10,
      priorityBoost: target.priorityBoost ?? 20,
    });
    setMetadataModalVisible(true);
  };

  const handleSaveMetadata = () => {
    metadataForm.validateFields().then((values) => {
      if (!selectedApp) return;

      // Only send the fields that are part of UpdateEndpointConfigRequest
      const config: UpdateEndpointConfigRequest = {
        displayName: values.displayName,
        description: values.description,
        taskTimeout: values.taskTimeout,
        minReplicas: values.minReplicas,
        maxReplicas: values.maxReplicas,
        priority: values.priority,
        scaleUpThreshold: values.scaleUpThreshold,
        scaleDownIdleTime: values.scaleDownIdleTime,
        scaleUpCooldown: values.scaleUpCooldown,
        scaleDownCooldown: values.scaleDownCooldown,
        enableDynamicPrio: values.enableDynamicPrio,
        highLoadThreshold: values.highLoadThreshold,
        priorityBoost: values.priorityBoost,
        autoscalerEnabled: values.autoscalerEnabled || undefined,
      };

      updateMetadataMutation.mutate({ name: selectedApp, config });
    });
  };

  const selectedAppInfo = useMemo(() => {
    if (!selectedApp) return null;
    return appDetails || apps?.find((app) => app.name === selectedApp) || null;
  }, [selectedApp, appDetails, apps]);

  const columns = [
    {
      title: 'Name',
      dataIndex: 'name',
      key: 'name',
      width: 200,
      fixed: 'left' as const,
      render: (name: string, record: AppInfo) => (
        <Space direction="vertical" size={0}>
          <Tooltip title={name}>
            <Text strong ellipsis style={{ maxWidth: 180 }}>
              {name}
            </Text>
          </Tooltip>
          {record.displayName && record.displayName !== name && (
            <Tooltip title={record.displayName}>
              <Text type="secondary" ellipsis style={{ fontSize: 12, maxWidth: 180 }}>
                {record.displayName}
              </Text>
            </Tooltip>
          )}
        </Space>
      ),
    },
    {
      title: 'Status',
      dataIndex: 'status',
      key: 'status',
      width: 160,
      fixed: 'left' as const,
      render: (status: string, record: AppInfo) => {
        let color = 'default';

        if (status === 'Running') {
          color = 'success';
        } else if (status === 'Pending') {
          color = 'warning';
        } else if (status === 'Failed' || status === 'Error') {
          color = 'error';
        }

        return (
          <Space direction="vertical" size={0}>
            <Badge color={color} text={status} />
            <Text type="secondary" style={{ fontSize: 12 }}>
              {record.readyReplicas}/{record.replicas} ready
            </Text>
          </Space>
        );
      },
    },
    {
      title: 'Spec',
      dataIndex: 'specName',
      key: 'specName',
      width: 120,
      render: (specName: string) => (
        <Tooltip title={specName || 'N/A'}>
          <Tag style={{ maxWidth: 100, overflow: 'hidden', textOverflow: 'ellipsis' }}>
            {specName || 'N/A'}
          </Tag>
        </Tooltip>
      ),
    },
    {
      title: 'Image',
      dataIndex: 'image',
      key: 'image',
      width: 250,
      render: (image: string) => (
        <Tooltip title={image}>
          <Text ellipsis style={{ maxWidth: 200 }}>
            {image || 'N/A'}
          </Text>
        </Tooltip>
      ),
    },
    {
      title: 'Replicas',
      dataIndex: 'replicas',
      key: 'replicas',
      width: 120,
      render: (replicas: number, record: AppInfo) => (
        <Space direction="vertical" size={0}>
          <Text strong>{replicas}</Text>
          <Text type="secondary" style={{ fontSize: 12 }}>
            (min: {record.minReplicas ?? 0}, max: {record.maxReplicas ?? 'Not Set'})
          </Text>
        </Space>
      ),
    },
    {
      title: 'Priority',
      dataIndex: 'priority',
      key: 'priority',
      width: 80,
      render: (priority: number) => (
        <Tag color={priority >= 70 ? 'red' : priority >= 50 ? 'orange' : 'default'}>
          {priority ?? '-'}
        </Tag>
      ),
    },
    {
      title: 'AutoScaler',
      dataIndex: 'autoscalerEnabled',
      key: 'autoscalerEnabled',
      width: 110,
      render: (autoscalerEnabled: string | undefined) => {
        if (!autoscalerEnabled || autoscalerEnabled === '') {
          return (
            <Tooltip title="Follow global autoscaler setting">
              <Tag color="default" icon={<GlobalOutlined />}>
                Default
              </Tag>
            </Tooltip>
          );
        } else if (autoscalerEnabled === 'disabled') {
          return (
            <Tooltip title="Autoscaler disabled for this endpoint">
              <Tag color="red" icon={<CloseCircleOutlined />}>
                Force Off
              </Tag>
            </Tooltip>
          );
        } else if (autoscalerEnabled === 'enabled') {
          return (
            <Tooltip title="Autoscaler enabled for this endpoint">
              <Tag color="green" icon={<CheckCircleOutlined />}>
                Force On
              </Tag>
            </Tooltip>
          );
        }
        return <Tag>Unknown</Tag>;
      },
    },
    {
      title: 'Tasks',
      key: 'tasks',
      width: 120,
      render: (_: any, record: AppInfo) => (
        <Space size="small">
          {record.pendingTasks !== undefined && record.pendingTasks > 0 && (
            <Tooltip title="Pending Tasks">
              <Tag color="orange" icon={<ClockCircleOutlined />}>
                {record.pendingTasks}
              </Tag>
            </Tooltip>
          )}
          {record.runningTasks !== undefined && record.runningTasks > 0 && (
            <Tooltip title="Running Tasks">
              <Tag color="blue" icon={<SyncOutlined spin />}>
                {record.runningTasks}
              </Tag>
            </Tooltip>
          )}
          {(!record.pendingTasks || record.pendingTasks === 0) &&
            (!record.runningTasks || record.runningTasks === 0) && (
              <Text type="secondary">-</Text>
            )}
        </Space>
      ),
    },
    {
      title: 'Actions',
      key: 'actions',
      fixed: 'right' as const,
      width: 150,
      render: (_: any, record: AppInfo) => {
        const lastTriggered = triggerAutoscalerMutation.variables as string | undefined;
        const isTriggering = triggerAutoscalerMutation.isPending && lastTriggered === record.name;
        const menuItems: MenuProps['items'] = [
          {
            key: 'trigger',
            label: 'Trigger AutoScaler',
            icon: <ThunderboltOutlined />,
            onClick: ({ domEvent }) => {
              domEvent.stopPropagation();
              triggerAutoscalerMutation.mutate(record.name);
            },
          },
          {
            type: 'divider',
          },
          {
            key: 'delete',
            label: 'Delete',
            danger: true,
            icon: <DeleteOutlined />,
            onClick: ({ domEvent }) => {
              domEvent.stopPropagation();
              Modal.confirm({
                title: 'Delete this app?',
                content: `Are you sure you want to delete "${record.name}"?`,
                okText: 'Delete',
                okType: 'danger',
                onOk: () => deleteMutation.mutate(record.name),
              });
            },
          },
        ];
        return (
          <Space size={4}>
            <Tooltip title="View Details">
              <Button
                size="small"
                type="text"
                icon={<FileTextOutlined />}
                onClick={(e) => {
                  e.stopPropagation();
                  setSelectedApp(record.name);
                  setDetailsDrawerVisible(true);
                }}
              />
            </Tooltip>
            <Tooltip title="Update Deployment">
              <Button
                size="small"
                type="text"
                icon={<EditOutlined />}
                onClick={(e) => {
                  e.stopPropagation();
                  setSelectedApp(record.name);
                  updateForm.setFieldsValue({
                    specName: record.specName || '',
                    image: record.image || '',
                    replicas: record.replicas || 1,
                  });
                  setUpdateModalVisible(true);
                }}
              />
            </Tooltip>
            <Tooltip title="Edit & Scale">
              <Button
                size="small"
                type="text"
                icon={<SettingOutlined />}
                onClick={(e) => {
                  e.stopPropagation();
                  handleEditMetadata(record);
                }}
              />
            </Tooltip>
            <Dropdown menu={{ items: menuItems }} trigger={['click']}>
              <Button
                size="small"
                type="text"
                icon={<EllipsisOutlined />}
                loading={isTriggering}
                onClick={(e) => e.stopPropagation()}
              />
            </Dropdown>
          </Space>
        );
      },
    },
  ];

  // Render cluster resources panel
  const renderClusterResourcesPanel = () => {
    if (!clusterStatus) return null;

    const { clusterResources } = clusterStatus;
    const gpuPercent = clusterResources.total.gpuCount > 0
      ? (clusterResources.used.gpuCount / clusterResources.total.gpuCount) * 100
      : 0;
    const cpuPercent = clusterResources.total.cpuCores > 0
      ? (clusterResources.used.cpuCores / clusterResources.total.cpuCores) * 100
      : 0;
    const memPercent = clusterResources.total.memoryGB > 0
      ? (clusterResources.used.memoryGB / clusterResources.total.memoryGB) * 100
      : 0;

    return (
      <Card size="small" style={{ marginTop: 16 }}>
        <Space direction="vertical" style={{ width: '100%' }} size="middle">
          <Row justify="space-between" align="middle">
            <Col>
              <Space>
                <CloudOutlined style={{ fontSize: 20, color: '#1890ff' }} />
                <Title level={5} style={{ margin: 0 }}>
                  Cluster Resources
                </Title>
              </Space>
            </Col>
            <Col>
              <Space>
                <Badge
                  status={clusterStatus.enabled ? 'success' : 'default'}
                  text={clusterStatus.enabled ? 'Enabled' : 'Disabled'}
                />
                <Tooltip title="Last run time">
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    {clusterStatus.lastRunTime
                      ? new Date(clusterStatus.lastRunTime).toLocaleTimeString()
                      : 'Never'}
                  </Text>
                </Tooltip>
              </Space>
            </Col>
          </Row>

          <Row gutter={16}>
            <Col span={8}>
              <Statistic
                title="GPU"
                value={clusterResources.used.gpuCount}
                suffix={`/ ${clusterResources.total.gpuCount}`}
              />
              <Progress percent={Number(gpuPercent.toFixed(1))} status="active" />
            </Col>
            <Col span={8}>
              <Statistic
                title="CPU Cores"
                value={clusterResources.used.cpuCores.toFixed(1)}
                suffix={
                  clusterResources.total.cpuCores > 0
                    ? `/ ${clusterResources.total.cpuCores}`
                    : '/ Unlimited'
                }
              />
              <Progress
                percent={clusterResources.total.cpuCores > 0 ? Number(cpuPercent.toFixed(1)) : 0}
                status="active"
              />
            </Col>
            <Col span={8}>
              <Statistic
                title="Memory (GB)"
                value={clusterResources.used.memoryGB.toFixed(1)}
                suffix={
                  clusterResources.total.memoryGB > 0
                    ? `/ ${clusterResources.total.memoryGB}`
                    : '/ Unlimited'
                }
              />
              <Progress
                percent={clusterResources.total.memoryGB > 0 ? Number(memPercent.toFixed(1)) : 0}
                status="active"
              />
            </Col>
          </Row>

          {globalConfig && (
            <Alert
              message={
                <Space>
                  <Text>Config:</Text>
                  <Text code>Interval: {globalConfig.interval}s</Text>
                  <Text code>Starvation: {globalConfig.starvationTime}s</Text>
                </Space>
              }
              type="info"
              showIcon
            />
          )}
        </Space>
      </Card>
    );
  };

  // Render recent events panel
  const renderRecentEventsPanel = () => {
    if (!recentEvents || recentEvents.length === 0) {
      return (
        <Card size="small" style={{ marginTop: 16 }}>
          <Empty description="No recent scaling events" />
        </Card>
      );
    }

    return (
      <Card size="small" style={{ marginTop: 16 }}>
        <Space direction="vertical" style={{ width: '100%' }}>
          <Space>
            <HistoryOutlined style={{ fontSize: 20, color: '#52c41a' }} />
            <Title level={5} style={{ margin: 0 }}>
              Recent Scaling Events
            </Title>
          </Space>
          <Timeline
            items={recentEvents.slice(0, 5).map((event) => ({
              color:
                event.action === 'scale_up' ? 'green' : event.action === 'scale_down' ? 'blue' : 'gray',
              children: (
                <Space direction="vertical" size={0}>
                  <Space>
                    <Tag color="blue">{event.endpoint}</Tag>
                    {event.action === 'scale_up' ? (
                      <Tag color="green" icon={<RiseOutlined />}>
                        Scale Up
                      </Tag>
                    ) : (
                      <Tag color="blue" icon={<FallOutlined />}>
                        Scale Down
                      </Tag>
                    )}
                    <Text strong>
                      {event.fromReplicas} → {event.toReplicas}
                    </Text>
                  </Space>
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    {event.reason} (Queue: {event.queueLength})
                  </Text>
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    {new Date(event.timestamp).toLocaleString()}
                  </Text>
                </Space>
              ),
            }))}
          />
        </Space>
      </Card>
    );
  };

  return (
    <div>
      <Row justify="space-between" align="middle" style={{ marginBottom: 12 }}>
        <Col>
          <Title level={3} style={{ margin: 0, marginBottom: 4 }}>
            <CloudOutlined /> Applications
          </Title>
          <Paragraph type="secondary" style={{ margin: 0, fontSize: 13 }}>
            Monitor deployments and AutoScaler state
          </Paragraph>
        </Col>
        <Col>
          <Space wrap>
            <Space>
              <Text strong>AutoScaler</Text>
              <Tooltip title="Control global AutoScaler scaling strategy">
                <Switch
                  checked={clusterStatus?.enabled ?? false}
                  loading={toggleAutoscalerMutation.isPending}
                  onChange={(checked) => toggleAutoscalerMutation.mutate(checked)}
                  checkedChildren="On"
                  unCheckedChildren="Off"
                />
              </Tooltip>
            </Space>
            <Button icon={<SettingOutlined />} onClick={() => setGlobalConfigModalVisible(true)}>
              Global Config
            </Button>
            <Button
              icon={<ThunderboltOutlined />}
              onClick={() => triggerAutoscalerMutation.mutate(undefined)}
              loading={triggerAutoscalerMutation.isPending}
            >
              Trigger All
            </Button>
            <Button
              type="primary"
              icon={<ReloadOutlined />}
              onClick={handleManualRefresh}
              loading={isManualRefreshing}
            >
              Refresh
            </Button>
          </Space>
        </Col>
      </Row>

      {/* AutoScaler Overview */}
      {renderClusterResourcesPanel()}

      {/* Applications Table */}
      <Card style={{ marginTop: 12 }}>
        <Table
          columns={columns}
          dataSource={apps || []}
          rowKey="name"
          loading={isManualRefreshing}
          scroll={{ x: 1400 }}
          pagination={{ pageSize: 20 }}
          expandable={{
            expandedRowRender: (record: AppInfo) => (
              <div
                style={{
                  padding: '16px 24px',
                  background: '#fafafa',
                  border: '1px solid #e8e8e8',
                  borderRadius: '4px',
                  margin: '8px 0',
                }}
              >
                <EndpointDetailsTabs key={`details-${record.name}`} endpoint={record.name} />
              </div>
            ),
            expandedRowKeys,
            expandRowByClick: true,
            onExpand: (expanded, record) => {
              setExpandedRowKeys(expanded ? [record.name] : []);
            },
          }}
        />
      </Card>

      {/* Recent Events */}
      {renderRecentEventsPanel()}

      {/* Details Drawer */}
      <Drawer
        title={`Endpoint Details: ${selectedApp || '-'}`}
        open={detailsDrawerVisible}
        width="60%"
        onClose={() => {
          setDetailsDrawerVisible(false);
          if (!metadataModalVisible) {
            setSelectedApp('');
          }
        }}
        extra={
          <Button
            type="primary"
            icon={<EditOutlined />}
            disabled={!selectedAppInfo}
            onClick={() => handleEditMetadata(selectedAppInfo || undefined)}
          >
            Edit & Scale
          </Button>
        }
      >
        {selectedAppInfo ? (
          <Space direction="vertical" size="large" style={{ width: '100%' }}>
            <Descriptions title="Basic Info" bordered column={1} size="small">
              <Descriptions.Item label="Name">{selectedAppInfo.name}</Descriptions.Item>
              <Descriptions.Item label="Display Name">
                {selectedAppInfo.displayName || selectedAppInfo.name}
              </Descriptions.Item>
              <Descriptions.Item label="Task Timeout (seconds)">
                {formatTaskTimeout(selectedAppInfo.taskTimeout)}
              </Descriptions.Item>
              <Descriptions.Item label="Namespace">{selectedAppInfo.namespace || '-'}</Descriptions.Item>
              <Descriptions.Item label="Status">
                <Badge
                  status={
                    selectedAppInfo.status === 'Running'
                      ? 'success'
                      : selectedAppInfo.status === 'Pending'
                      ? 'processing'
                      : 'error'
                  }
                  text={selectedAppInfo.status}
                />
              </Descriptions.Item>
              <Descriptions.Item label="Spec">{selectedAppInfo.specName || '-'}</Descriptions.Item>
              <Descriptions.Item label="Image">{selectedAppInfo.image || '-'}</Descriptions.Item>
              <Descriptions.Item label="Replicas">
                {selectedAppInfo.readyReplicas || 0}/{selectedAppInfo.replicas || 0}
                {selectedAppInfo.minReplicas === 0 && (
                  <Text type="secondary" style={{ marginLeft: 8 }}>
                    min 0 → can fully scale down when idle
                  </Text>
                )}
              </Descriptions.Item>
              <Descriptions.Item label="Created At">
                {selectedAppInfo.createdAt ? new Date(selectedAppInfo.createdAt).toLocaleString() : '-'}
              </Descriptions.Item>
              <Descriptions.Item label="Updated At">
                {selectedAppInfo.updatedAt ? new Date(selectedAppInfo.updatedAt).toLocaleString() : '-'}
              </Descriptions.Item>
            </Descriptions>

            <Descriptions title="AutoScaler Config" bordered column={1} size="small">
              <Descriptions.Item label="AutoScaler Override">
                {!selectedAppInfo.autoscalerEnabled || selectedAppInfo.autoscalerEnabled === '' ? (
                  <Tag color="default" icon={<GlobalOutlined />}>
                    Default (follow global)
                  </Tag>
                ) : selectedAppInfo.autoscalerEnabled === 'disabled' ? (
                  <Tag color="red" icon={<CloseCircleOutlined />}>
                    Force Off
                  </Tag>
                ) : selectedAppInfo.autoscalerEnabled === 'enabled' ? (
                  <Tag color="green" icon={<CheckCircleOutlined />}>
                    Force On
                  </Tag>
                ) : (
                  <Tag>Unknown</Tag>
                )}
              </Descriptions.Item>
              <Descriptions.Item label={renderFieldLabel('Priority', 'priority')}>
                {selectedAppInfo.priority ?? 50}
                {renderZeroHint(selectedAppInfo.priority === 0, '0 = lowest priority (best-effort)')}
              </Descriptions.Item>
              <Descriptions.Item label="Min Replicas">
                {selectedAppInfo.minReplicas ?? 0}
                {renderZeroHint(selectedAppInfo.minReplicas === 0, 'scale-to-zero enabled')}
              </Descriptions.Item>
              <Descriptions.Item label="Max Replicas">
                {selectedAppInfo.maxReplicas ?? 'Not Set'}
                {selectedAppInfo.maxReplicas === 0 && renderZeroHint(true, 'maxReplicas=0 will disable autoscaling!')}
              </Descriptions.Item>
              <Descriptions.Item label={renderFieldLabel('Scale Up Threshold', 'scaleUpThreshold')}>
                {selectedAppInfo.scaleUpThreshold ?? 1}
                {renderZeroHint(selectedAppInfo.scaleUpThreshold === 0, '0 behaves like 1 (scale on first task)')}
              </Descriptions.Item>
              <Descriptions.Item label={renderFieldLabel('Scale Down Idle Time', 'scaleDownIdleTime')}>
                {selectedAppInfo.scaleDownIdleTime ?? 300}s
                {renderZeroHint(selectedAppInfo.scaleDownIdleTime === 0, '0 = scale down immediately after idle')}
              </Descriptions.Item>
              <Descriptions.Item label={renderFieldLabel('Scale Up Cooldown', 'scaleUpCooldown')}>
                {typeof selectedAppInfo.scaleUpCooldown === 'number' ? `${selectedAppInfo.scaleUpCooldown}s` : '-'}
                {renderZeroHint(selectedAppInfo.scaleUpCooldown === 0, '0 = no cooldown between scale ups')}
              </Descriptions.Item>
              <Descriptions.Item label={renderFieldLabel('Scale Down Cooldown', 'scaleDownCooldown')}>
                {typeof selectedAppInfo.scaleDownCooldown === 'number' ? `${selectedAppInfo.scaleDownCooldown}s` : '-'}
                {renderZeroHint(selectedAppInfo.scaleDownCooldown === 0, '0 = no cooldown between scale downs')}
              </Descriptions.Item>
              <Descriptions.Item label="Dynamic Priority">
                {selectedAppInfo.enableDynamicPrio ? 'Enabled' : 'Disabled'}
              </Descriptions.Item>
              {selectedAppInfo.enableDynamicPrio && (
                <>
                  <Descriptions.Item label={renderFieldLabel('High Load Threshold', 'highLoadThreshold')}>
                    {selectedAppInfo.highLoadThreshold ?? '-'}
                    {renderZeroHint(selectedAppInfo.highLoadThreshold === 0, '0 = always treated as high load')}
                  </Descriptions.Item>
                  <Descriptions.Item label={renderFieldLabel('Priority Boost', 'priorityBoost')}>
                    {selectedAppInfo.priorityBoost ?? '-'}
                    {renderZeroHint(selectedAppInfo.priorityBoost === 0, '0 = no boost even under high load')}
                  </Descriptions.Item>
                </>
              )}
            </Descriptions>
          </Space>
        ) : (
          <Text type="secondary">Select an application to view details.</Text>
        )}
      </Drawer>

      {/* Update Deployment Modal */}
      <Modal
        title="Update Deployment"
        open={updateModalVisible}
        onOk={() => updateForm.submit()}
        onCancel={() => {
          setUpdateModalVisible(false);
          updateForm.resetFields();
        }}
        confirmLoading={updateMutation.isPending}
      >
        <Form form={updateForm} layout="vertical" onFinish={handleUpdate}>
          <Form.Item label="Spec" name="specName">
            <Select
              placeholder="Select spec"
              allowClear
              showSearch
              options={specs?.map((spec) => ({
                label: `${spec.name} (${spec.category})`,
                value: spec.name,
              }))}
            />
          </Form.Item>
          <Form.Item label="Image" name="image">
            <Input placeholder="registry/image:tag" />
          </Form.Item>
          <Form.Item label="Replicas" name="replicas">
            <InputNumber min={0} max={100} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>

      {/* Update Metadata Modal */}
      <Modal
        title="Edit Metadata & AutoScaler Config"
        open={metadataModalVisible}
        onOk={handleSaveMetadata}
        onCancel={() => {
          setMetadataModalVisible(false);
          metadataForm.resetFields();
        }}
        confirmLoading={updateMetadataMutation.isPending}
        width={600}
      >
        <Form form={metadataForm} layout="vertical">
          <Divider>Basic Information</Divider>
          <Form.Item label="Display Name" name="displayName">
            <Input placeholder="Friendly display name" />
          </Form.Item>
          <Form.Item label="Description" name="description">
            <TextArea rows={2} placeholder="Application description" />
          </Form.Item>
          <Form.Item
            label="Task Timeout (seconds)"
            name="taskTimeout"
            help={`0 = use global default (${GLOBAL_TASK_TIMEOUT}s)`}
          >
            <InputNumber min={0} placeholder={`0 = use global default (${GLOBAL_TASK_TIMEOUT}s)`} style={{ width: '100%' }} />
          </Form.Item>

          <Divider>AutoScaler Configuration</Divider>
          <Form.Item
            label="AutoScaler Override"
            name="autoscalerEnabled"
            help="Control autoscaling for this endpoint independently of global setting"
            tooltip="Default = follow global setting, Force Off = always disabled, Force On = always enabled"
          >
            <Select
              allowClear
              placeholder="Default (follow global)"
              options={[
                { value: '', label: 'Default (follow global)' },
                { value: 'disabled', label: 'Force Off' },
                { value: 'enabled', label: 'Force On' },
              ]}
            />
          </Form.Item>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item
                label="Min Replicas"
                name="minReplicas"
                help="Set to 0 to allow scale-to-zero when idle"
              >
                <InputNumber min={0} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item label="Max Replicas" name="maxReplicas">
                <InputNumber min={1} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
          </Row>
          <Form.Item
            label={renderFieldLabel('Priority', 'priority')}
            name="priority"
            help="0 = lowest priority (best-effort). Higher values reserve resources sooner."
          >
            <InputNumber min={0} max={100} style={{ width: '100%' }} />
          </Form.Item>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item
                label={renderFieldLabel('Scale Up Threshold', 'scaleUpThreshold')}
                name="scaleUpThreshold"
                help="0 behaves like 1 (scale as soon as a task waits)."
              >
                <InputNumber min={1} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item
                label={renderFieldLabel('Scale Down Idle Time (s)', 'scaleDownIdleTime')}
                name="scaleDownIdleTime"
                help="0 = immediate scale down when idle."
              >
                <InputNumber min={60} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item
                label={renderFieldLabel('Scale Up Cooldown (s)', 'scaleUpCooldown')}
                name="scaleUpCooldown"
                help="0 = no cooldown between scale ups."
              >
                <InputNumber min={0} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item
                label={renderFieldLabel('Scale Down Cooldown (s)', 'scaleDownCooldown')}
                name="scaleDownCooldown"
                help="0 = no cooldown between scale downs."
              >
                <InputNumber min={0} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
          </Row>
          <Form.Item
            label="Enable Dynamic Priority"
            name="enableDynamicPrio"
            valuePropName="checked"
            tooltip="Automatically boost priority when queue is long"
          >
            <Switch />
          </Form.Item>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item
                label={renderFieldLabel('High Load Threshold', 'highLoadThreshold')}
                name="highLoadThreshold"
                tooltip="Queue length to trigger priority boost"
                help="0 = always considered high load."
              >
                <InputNumber min={1} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item
                label={renderFieldLabel('Priority Boost', 'priorityBoost')}
                name="priorityBoost"
                tooltip="Priority increase when high load threshold met"
                help="0 = no priority bump even under high load."
              >
                <InputNumber min={0} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
          </Row>
        </Form>
      </Modal>

      {/* Global AutoScaler Config Modal */}
      <Modal
        title="Global AutoScaler Configuration"
        open={globalConfigModalVisible}
        onCancel={() => setGlobalConfigModalVisible(false)}
        onOk={() => globalConfigForm.submit()}
        confirmLoading={updateGlobalConfigMutation.isPending}
        width={600}
      >
        <Form
          form={globalConfigForm}
          layout="vertical"
          onFinish={(values: GlobalConfig) => updateGlobalConfigMutation.mutate(values)}
        >
          <Divider>Control Loop</Divider>
          <Form.Item
            name="interval"
            label="Interval (seconds)"
            rules={[{ required: true, type: 'number', min: 1 }]}
            tooltip="How often the autoscaler evaluates workloads"
          >
            <InputNumber min={1} max={300} style={{ width: '100%' }} />
          </Form.Item>

          <Divider>Cluster Limits</Divider>
          <Row gutter={16}>
            <Col span={8}>
              <Form.Item
                name="maxGpuCount"
                label="Max GPU Count"
                rules={[{ required: true, type: 'number', min: 0 }]}
              >
                <InputNumber min={0} max={1000} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item
                name="maxCpuCores"
                label="Max CPU"
                help="0 = unlimited"
                rules={[{ required: true, type: 'number', min: 0 }]}
              >
                <InputNumber min={0} max={10000} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item
                name="maxMemoryGB"
                label="Max Memory (GB)"
                help="0 = unlimited"
                rules={[{ required: true, type: 'number', min: 0 }]}
              >
                <InputNumber min={0} max={100000} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
          </Row>

          <Divider>Starvation Protection</Divider>
          <Form.Item
            name="starvationTime"
            label="Starvation Time (seconds)"
            tooltip="After this idle time, endpoints receive a priority boost"
            rules={[{ required: true, type: 'number', min: 0 }]}
          >
            <InputNumber min={0} max={3600} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>
      {/* Legacy drawers/modals removed in favor of row-level tabs */}
    </div>
  );
};

interface EndpointDetailsTabsProps {
  endpoint: string;
}

const EndpointDetailsTabs = ({ endpoint }: EndpointDetailsTabsProps) => {
  const [taskDetail, setTaskDetail] = useState<Task | null>(null);
  const [taskDrawerVisible, setTaskDrawerVisible] = useState(false);
  const [workerDrawerVisible, setWorkerDrawerVisible] = useState(false);
  const [workerDrawerTab, setWorkerDrawerTab] = useState<'logs' | 'terminal'>('terminal');
  const [selectedWorker, setSelectedWorker] = useState<{ endpoint: string; workerId: string } | null>(null);
  const [hasEverOpenedTerminal, setHasEverOpenedTerminal] = useState(false);
  const [podDescribeDrawerVisible, setPodDescribeDrawerVisible] = useState(false);
  const [selectedPod, setSelectedPod] = useState<{ endpoint: string; podName: string } | null>(null);

  // Fetch task events when task is selected
  const { data: eventsData } = useQuery({
    queryKey: ['task-events', taskDetail?.id],
    queryFn: async () => {
      if (!taskDetail?.id) return null;
      const response = await api.tasks.getEvents(taskDetail.id);
      return response.data;
    },
    enabled: !!taskDetail?.id && taskDrawerVisible,
  });

  // Fetch task timeline
  const { data: timelineData } = useQuery({
    queryKey: ['task-timeline', taskDetail?.id],
    queryFn: async () => {
      if (!taskDetail?.id) return null;
      const response = await api.tasks.getTimeline(taskDetail.id);
      return response.data;
    },
    enabled: !!taskDetail?.id && taskDrawerVisible,
  });

  // Fetch task execution history
  const { data: executionHistory } = useQuery({
    queryKey: ['task-execution-history', taskDetail?.id],
    queryFn: async () => {
      if (!taskDetail?.id) return null;
      const response = await api.tasks.getExecutionHistory(taskDetail.id);
      return response.data;
    },
    enabled: !!taskDetail?.id && taskDrawerVisible,
  });
  // Helper functions for task status display
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
        return <LoadingOutlined />;
      default:
        return null;
    }
  };

  const { data: history, isFetching: loadingHistory} = useQuery({
    queryKey: ['autoscaler-history', endpoint, 'row'],
    queryFn: async () => {
      const response = await api.autoscaler.getHistory(endpoint, 20);
      return response.data;
    },
    enabled: !!endpoint,
    staleTime: 20000,
  });

  const { data: tasksResponse, isFetching: loadingTasks } = useQuery({
    queryKey: ['tasks', endpoint, 'row'],
    queryFn: async () => {
      const response = await api.tasks.list({ endpoint, limit: 100 });
      return response.data;
    },
    enabled: !!endpoint,
    refetchInterval: 5000,
  });

  const tasks = tasksResponse?.tasks || [];

  const { data: logsText, isFetching: loadingLogs, refetch: refetchLogs } = useQuery({
    queryKey: ['app-logs', endpoint, selectedWorker?.workerId],
    queryFn: async () => {
      const response = await api.apps.logs(endpoint, 200, selectedWorker?.workerId);
      return response.data;
    },
    enabled: !!endpoint && workerDrawerVisible && workerDrawerTab === 'logs' && !!selectedWorker,
    refetchInterval: 10000,
  });

  const { data: workers, isFetching: loadingWorkers } = useQuery({
    queryKey: ['workers', endpoint, 'row'],
    queryFn: async () => {
      const response = await api.apps.workers(endpoint);
      return response.data;
    },
    enabled: !!endpoint,
    refetchInterval: 5000,
  });

  // Fetch Pod describe details
  const { data: podDetail, isFetching: loadingPodDetail } = useQuery({
    queryKey: ['pod-describe', selectedPod?.endpoint, selectedPod?.podName],
    queryFn: async () => {
      if (!selectedPod) return null;
      const response = await api.apps.describePod(selectedPod.endpoint, selectedPod.podName);
      return response.data;
    },
    enabled: !!selectedPod && podDescribeDrawerVisible,
  });

  const queueStats = useMemo(() => {
    if (!tasks || tasks.length === 0) {
      return {
        total: 0,
        pending: 0,
        inProgress: 0,
        completed: 0,
        failed: 0,
        cancelled: 0,
      };
    }
    return {
      total: tasks.length,
      pending: tasks.filter((t: Task) => t.status === 'PENDING').length,
      inProgress: tasks.filter((t: Task) => t.status === 'IN_PROGRESS').length,
      completed: tasks.filter((t: Task) => t.status === 'COMPLETED').length,
      failed: tasks.filter((t: Task) => t.status === 'FAILED').length,
      cancelled: tasks.filter((t: Task) => t.status === 'CANCELLED').length,
    };
  }, [tasks]);

  const pendingTasks = useMemo(
    () => (tasks ? tasks.filter((t: Task) => t.status === 'PENDING') : []),
    [tasks]
  );

  const taskColumns = [
    {
      title: 'Task ID',
      dataIndex: 'id',
      key: 'id',
      render: (id: string) => (
        <Tooltip title={id}>
          <Text code style={{ fontSize: 12 }}>
            {id.substring(0, 10)}...
          </Text>
        </Tooltip>
      ),
      ellipsis: true,
    },
    {
      title: 'Status',
      dataIndex: 'status',
      key: 'status',
      render: (status: string) => {
        const colorMap: Record<string, string> = {
          PENDING: 'orange',
          IN_PROGRESS: 'blue',
          COMPLETED: 'green',
          FAILED: 'red',
          CANCELLED: 'default',
        };
        return <Tag color={colorMap[status] || 'default'}>{status}</Tag>;
      },
    },
    {
      title: 'Worker',
      dataIndex: 'workerId',
      key: 'workerId',
      render: (worker: string | undefined) => worker ? (
        <Tooltip title={worker}>
          <Text code style={{ fontSize: 12 }}>{worker.substring(0, 12)}...</Text>
        </Tooltip>
      ) : '-',
      ellipsis: true,
    },
    {
      title: 'Created At',
      dataIndex: 'createdAt',
      key: 'createdAt',
      render: (time: string) => (time ? new Date(time).toLocaleString() : '-'),
    },
    {
      title: 'Details',
      key: 'details',
      render: (_: any, task: Task) => (
        <Button
          size="small"
          onClick={() => {
            setTaskDetail(task);
            setTaskDrawerVisible(true);
          }}
        >
          View
        </Button>
      ),
    },
  ];

  const workerColumns = [
    {
      title: 'Worker / Pod',
      dataIndex: 'id',
      key: 'id',
      width: 250,
      render: (_: string, record: WorkerWithPodInfo) => (
        <Space direction="vertical" size={0}>
          {record.id ? (
            <Tooltip title={record.id}>
              <Text code style={{ fontSize: 12 }}>
                {record.id.substring(0, 24)}
              </Text>
            </Tooltip>
          ) : (
            <Text type="secondary" style={{ fontSize: 12 }}>No Worker ID</Text>
          )}
          {record.pod_name && record.pod_name !== record.id && (
            <Tooltip title={record.pod_name}>
              <Text type="secondary" style={{ fontSize: 11 }}>
                Pod: {record.pod_name.substring(0, 20)}...
              </Text>
            </Tooltip>
          )}
        </Space>
      ),
    },
    {
      title: 'Status',
      dataIndex: 'status',
      key: 'status',
      width: 110,
      render: (_: string, record: WorkerWithPodInfo) => {
        // Worker status
        let workerColor = 'default';
        let workerIcon = <QuestionCircleOutlined />;
        const statusLower = record.status?.toLowerCase();
        if (statusLower === 'online') {
          workerColor = 'green';
          workerIcon = <CheckCircleOutlined />;
        } else if (statusLower === 'offline') {
          workerColor = 'red';
          workerIcon = <CloseCircleOutlined />;
        } else if (statusLower === 'busy') {
          workerColor = 'blue';
          workerIcon = <LoadingOutlined />;
        } else if (statusLower === 'draining') {
          workerColor = 'orange';
          workerIcon = <SyncOutlined spin />;
        }

        return (
          <Space direction="vertical" size={0}>
            <Tag color={workerColor} icon={workerIcon} style={{ fontSize: 11 }}>
              {record.status?.toUpperCase() || 'UNKNOWN'}
            </Tag>
            {record.podStatus && (
              <Tag
                color={
                  record.podStatus === 'Running' ? 'green' :
                  record.podStatus === 'Creating' || record.podStatus === 'Pending' ? 'orange' :
                  record.podStatus === 'Terminating' ? 'volcano' :
                  record.podStatus === 'Failed' ? 'red' : 'default'
                }
                style={{ fontSize: 10 }}
              >
                Pod: {record.podStatus}
              </Tag>
            )}
          </Space>
        );
      },
    },
    {
      title: 'Current Jobs',
      dataIndex: 'current_jobs',
      key: 'current_jobs',
      width: 90,
      render: (currentJobs: number, record: Worker) => (
        <Text>{currentJobs || 0} / {record.concurrency || 0}</Text>
      ),
    },
    {
      title: 'Last Heartbeat',
      dataIndex: 'last_heartbeat',
      key: 'last_heartbeat',
      width: 110,
      render: (time: string) => {
        if (!time) return '-';
        const heartbeat = new Date(time);
        const now = new Date();
        const diff = now.getTime() - heartbeat.getTime();
        const seconds = Math.floor(diff / 1000);
        if (seconds < 60) return `${seconds}s ago`;
        if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
        return heartbeat.toLocaleString();
      },
    },
    {
      title: 'Version',
      dataIndex: 'version',
      key: 'version',
      width: 80,
      render: (version: string) => version || '-',
    },
    {
      title: 'Actions',
      key: 'actions',
      width: 240,
      render: (_: any, record: WorkerWithPodInfo) => (
        <Space size="small">
          {(record.status?.toLowerCase() === 'online' || record.status?.toLowerCase() === 'busy') ? (
            <>
              <Button
                type="link"
                size="small"
                icon={<FileTextOutlined />}
                onClick={() => {
                  console.log('Logs button clicked', { endpoint: record.endpoint, workerId: record.id });
                  setSelectedWorker({ endpoint: record.endpoint, workerId: record.id });
                  setWorkerDrawerTab('logs');
                  setWorkerDrawerVisible(true);
                }}
              >
                Logs
              </Button>
              <Button
                type="link"
                size="small"
                icon={<CodeOutlined />}
                onClick={() => {
                  console.log('Exec button clicked', { endpoint: record.endpoint, workerId: record.id });
                  setSelectedWorker({ endpoint: record.endpoint, workerId: record.id });
                  setWorkerDrawerTab('terminal');
                  setWorkerDrawerVisible(true);
                  setHasEverOpenedTerminal(true);
                }}
              >
                Exec
              </Button>
            </>
          ) : null}
          {record.id && (
            <Button
              type="link"
              size="small"
              icon={<UnorderedListOutlined />}
              onClick={() => {
                console.log('Describe button clicked', { endpoint: record.endpoint, podName: record.id });
                setSelectedPod({ endpoint: record.endpoint, podName: record.id });
                setPodDescribeDrawerVisible(true);
              }}
            >
              Describe
            </Button>
          )}
        </Space>
      ),
    },
  ];

  const queueTabLabel = useMemo(
    () => (
      <Space size={4}>
        Queue & Tasks
        <Tooltip title="Auto-refresh every 5s">
          <SyncOutlined spin={loadingTasks} style={{ color: loadingTasks ? '#1890ff' : '#bfbfbf' }} />
        </Tooltip>
      </Space>
    ),
    [loadingTasks]
  );

  const pendingEmptyText =
    loadingTasks && pendingTasks.length === 0 ? 'Loading queue…' : 'No pending queue tasks';
  const taskEmptyText =
    loadingTasks && (!tasks || tasks.length === 0) ? 'Loading tasks…' : 'No tasks yet';

  const tabs = [
    {
      key: 'queue',
      label: queueTabLabel,
      children: (
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Row gutter={16}>
            <Col xs={12} md={4}>
              <Statistic title="Pending" value={queueStats.pending} valueStyle={{ color: '#faad14' }} />
            </Col>
            <Col xs={12} md={4}>
              <Statistic title="In Progress" value={queueStats.inProgress} valueStyle={{ color: '#1890ff' }} />
            </Col>
            <Col xs={12} md={4}>
              <Statistic title="Completed" value={queueStats.completed} valueStyle={{ color: '#52c41a' }} />
            </Col>
            <Col xs={12} md={4}>
              <Statistic title="Failed" value={queueStats.failed} valueStyle={{ color: '#ff4d4f' }} />
            </Col>
            <Col xs={12} md={4}>
              <Statistic title="Cancelled" value={queueStats.cancelled} />
            </Col>
          </Row>

          <Title level={5} style={{ margin: '16px 0 8px' }}>
            Pending Queue
          </Title>
          <Table
            size="small"
            dataSource={pendingTasks}
            columns={taskColumns}
            rowKey="id"
            pagination={{ pageSize: 5 }}
            locale={{ emptyText: pendingEmptyText }}
          />

          <Title level={5} style={{ margin: '16px 0 8px' }}>
            All Tasks
          </Title>
          <Table
            size="small"
            dataSource={tasks || []}
            columns={taskColumns}
            rowKey="id"
            pagination={{ pageSize: 5 }}
            locale={{ emptyText: taskEmptyText }}
          />
        </Space>
      ),
    },
    {
      key: 'workers',
      label: (
        <Space size={4}>
          <UserOutlined /> Workers
          <Tooltip title="Auto-refresh every 5s">
            <SyncOutlined spin={loadingWorkers} style={{ color: loadingWorkers ? '#1890ff' : '#bfbfbf' }} />
          </Tooltip>
        </Space>
      ),
      children: (
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Row gutter={16}>
            <Col span={8}>
              <Statistic
                title="Total Workers"
                value={workers?.length || 0}
                prefix={<UserOutlined />}
              />
            </Col>
            <Col span={8}>
              <Statistic
                title="Online"
                value={workers?.filter((w: Worker) => w.status === 'online').length || 0}
                valueStyle={{ color: '#52c41a' }}
              />
            </Col>
            <Col span={8}>
              <Statistic
                title="Busy"
                value={workers?.filter((w: Worker) => w.status === 'busy').length || 0}
                valueStyle={{ color: '#1890ff' }}
              />
            </Col>
          </Row>
          <Table
            size="small"
            dataSource={workers || []}
            columns={workerColumns}
            rowKey="id"
            pagination={{ pageSize: 10 }}
            locale={{ emptyText: 'No workers' }}
          />
        </Space>
      ),
    },
    {
      key: 'scaling',
      label: 'Scaling Logs',
      children: loadingHistory ? (
        <Text type="secondary">Loading scaling events…</Text>
      ) : history && history.length > 0 ? (
        <Timeline
          items={history.map((event: ScalingEvent) => ({
            color: event.action === 'scale_up' ? 'green' : event.action === 'scale_down' ? 'blue' : 'orange',
            children: (
              <Space direction="vertical" size={0}>
                <Space>
                  <Tag>{event.action}</Tag>
                  <Text>
                    {event.fromReplicas} → {event.toReplicas}
                  </Text>
                </Space>
                <Text type="secondary">
                  {event.reason} (Queue: {event.queueLength}
                  {typeof event.priority === 'number' ? ` | Priority: ${event.priority}` : ''})
                </Text>
                <Text type="secondary" style={{ fontSize: 12 }}>
                  {new Date(event.timestamp).toLocaleString()}
                </Text>
              </Space>
            ),
          }))}
        />
      ) : (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="No scaling events yet" />
      ),
    },
  ];

  return (
    <>
      <Tabs size="small" defaultActiveKey="queue" items={tabs} />
      {/* Task Details Drawer - Same style as Tasks page */}
      <Drawer
        title={<Space><FileTextOutlined /> Task Details</Space>}
        placement="right"
        onClose={() => {
          setTaskDrawerVisible(false);
          setTaskDetail(null);
        }}
        open={taskDrawerVisible}
        width="70%"
      >
        {taskDetail && (
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
                        <Text code>{taskDetail.id}</Text>
                      </Descriptions.Item>
                      {taskDetail.endpoint && (
                        <Descriptions.Item label="Endpoint">
                          <Text strong>{taskDetail.endpoint}</Text>
                        </Descriptions.Item>
                      )}
                      <Descriptions.Item label="Status">
                        <Tag icon={getStatusIcon(taskDetail.status)} color={getStatusColor(taskDetail.status)}>
                          {taskDetail.status}
                        </Tag>
                      </Descriptions.Item>
                      {taskDetail.workerId && (
                        <Descriptions.Item label="Worker ID">
                          <Tooltip title={taskDetail.workerId}>
                            <Text code>{taskDetail.workerId}</Text>
                          </Tooltip>
                        </Descriptions.Item>
                      )}
                      {taskDetail.createdAt && (
                        <Descriptions.Item label="Created At">
                          {new Date(taskDetail.createdAt).toLocaleString()}
                        </Descriptions.Item>
                      )}
                      {taskDetail.delayTime !== undefined && (
                        <Descriptions.Item label="Delay Time">
                          {taskDetail.delayTime}ms
                        </Descriptions.Item>
                      )}
                      {taskDetail.executionTime !== undefined && (
                        <Descriptions.Item label="Execution Time">
                          {(taskDetail.executionTime / 1000).toFixed(2)}s
                        </Descriptions.Item>
                      )}
                    </Descriptions>

                    {taskDetail.input && Object.keys(taskDetail.input).length > 0 && (
                      <Card title="Input" style={{ marginTop: 16 }} size="small">
                        <pre style={{ maxHeight: 300, overflow: 'auto', fontSize: 12 }}>
                          {JSON.stringify(taskDetail.input, null, 2)}
                        </pre>
                      </Card>
                    )}

                    {taskDetail.output && Object.keys(taskDetail.output).length > 0 && (
                      <Card title="Output" style={{ marginTop: 16 }} size="small">
                        <pre style={{ maxHeight: 300, overflow: 'auto', fontSize: 12 }}>
                          {JSON.stringify(taskDetail.output, null, 2)}
                        </pre>
                      </Card>
                    )}

                    {taskDetail.error && (
                      <Card title="Error" style={{ marginTop: 16 }} size="small">
                        <Text type="danger" style={{ fontFamily: 'monospace', fontSize: 12 }}>
                          {taskDetail.error}
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
      <Drawer
        title={
          <Space>
            <UserOutlined />
            {selectedWorker
              ? `Worker: ${selectedWorker.workerId.substring(0, 20)}`
              : 'Worker Operations'}
          </Space>
        }
        open={workerDrawerVisible}
        onClose={() => {
          console.log('Worker drawer closing');
          setWorkerDrawerVisible(false);
          // 不清空 selectedWorker，这样 Terminal 连接保持活跃
        }}
        width="80%"
        styles={{ body: { padding: 0 } }}
      >
        {selectedWorker && (
          <Tabs
            activeKey={workerDrawerTab}
            onChange={(key) => setWorkerDrawerTab(key as 'logs' | 'terminal')}
            items={[
              {
                key: 'logs',
                label: (
                  <Space>
                    <FileTextOutlined />
                    Logs
                  </Space>
                ),
                children: (
                  <div style={{ padding: '16px' }}>
                    <Space direction="vertical" style={{ width: '100%' }}>
                      <Button
                        size="small"
                        icon={<ReloadOutlined />}
                        onClick={() => refetchLogs()}
                        loading={loadingLogs}
                      >
                        Refresh Logs
                      </Button>
                      <TextArea
                        value={logsText || 'Loading logs...'}
                        readOnly
                        autoSize={{ minRows: 25, maxRows: 35 }}
                        style={{ fontFamily: 'monospace', fontSize: 12 }}
                      />
                    </Space>
                  </div>
                ),
              },
              {
                key: 'terminal',
                label: (
                  <Space>
                    <CodeOutlined />
                    Terminal
                  </Space>
                ),
                children: null, // Terminal 在下面独立渲染
              },
            ]}
          />
        )}
        {/* Terminal 始终挂载，只是隐藏，这样连接不会断开 */}
        {selectedWorker && hasEverOpenedTerminal && (
          <div style={{
            height: 'calc(100vh - 110px)',
            display: workerDrawerTab === 'terminal' ? 'block' : 'none'
          }}>
            <Terminal
              endpoint={selectedWorker.endpoint}
              workerId={selectedWorker.workerId}
              onClose={() => {
                setWorkerDrawerVisible(false);
              }}
            />
          </div>
        )}
      </Drawer>

      {/* Pod Describe Drawer */}
      <Drawer
        title={
          <Space>
            <CloudOutlined />
            Pod Details: {selectedPod?.podName || '-'}
          </Space>
        }
        open={podDescribeDrawerVisible}
        onClose={() => {
          setPodDescribeDrawerVisible(false);
          setSelectedPod(null);
        }}
        width="80%"
        loading={loadingPodDetail}
      >
        {podDetail && (
          <Tabs
            defaultActiveKey="details"
            items={[
              {
                key: 'details',
                label: <Space><UnorderedListOutlined /> Details</Space>,
                children: (
          <Space direction="vertical" size="large" style={{ width: '100%' }}>
            {/* Basic Info */}
            <Card title="Basic Information" size="small">
              <Descriptions bordered column={2} size="small">
                <Descriptions.Item label="Name" span={2}>{podDetail.name}</Descriptions.Item>
                <Descriptions.Item label="Namespace">{podDetail.namespace}</Descriptions.Item>
                <Descriptions.Item label="UID">{podDetail.uid}</Descriptions.Item>
                <Descriptions.Item label="Phase">
                  <Tag color={
                    podDetail.phase === 'Running' ? 'green' :
                    podDetail.phase === 'Pending' ? 'orange' :
                    podDetail.phase === 'Succeeded' ? 'blue' :
                    podDetail.phase === 'Failed' ? 'red' : 'default'
                  }>
                    {podDetail.phase}
                  </Tag>
                </Descriptions.Item>
                <Descriptions.Item label="Status">
                  <Tag>{podDetail.status}</Tag>
                  {podDetail.reason && <Text type="secondary"> ({podDetail.reason})</Text>}
                </Descriptions.Item>
                <Descriptions.Item label="Pod IP">{podDetail.ip || '-'}</Descriptions.Item>
                <Descriptions.Item label="Node">{podDetail.nodeName || '-'}</Descriptions.Item>
                <Descriptions.Item label="Created At">
                  {new Date(podDetail.createdAt).toLocaleString()}
                </Descriptions.Item>
                <Descriptions.Item label="Started At">
                  {podDetail.startedAt ? new Date(podDetail.startedAt).toLocaleString() : '-'}
                </Descriptions.Item>
                <Descriptions.Item label="Restart Count">{podDetail.restartCount}</Descriptions.Item>
                {podDetail.deletionTimestamp && (
                  <Descriptions.Item label="Deletion Time" span={2}>
                    <Text type="danger">{new Date(podDetail.deletionTimestamp).toLocaleString()}</Text>
                  </Descriptions.Item>
                )}
                {podDetail.message && (
                  <Descriptions.Item label="Message" span={2}>
                    <Text type="secondary">{podDetail.message}</Text>
                  </Descriptions.Item>
                )}
              </Descriptions>
            </Card>

            {/* Labels */}
            {podDetail.labels && Object.keys(podDetail.labels).length > 0 && (
              <Card title={`Labels (${Object.keys(podDetail.labels).length})`} size="small">
                <Descriptions bordered column={1} size="small">
                  {Object.entries(podDetail.labels).map(([key, value]) => (
                    <Descriptions.Item key={key} label={<Text code>{key}</Text>}>
                      <Text code>{value}</Text>
                    </Descriptions.Item>
                  ))}
                </Descriptions>
              </Card>
            )}

            {/* Annotations */}
            {podDetail.annotations && Object.keys(podDetail.annotations).length > 0 && (
              <Card title={`Annotations (${Object.keys(podDetail.annotations).length})`} size="small">
                <Descriptions bordered column={1} size="small">
                  {Object.entries(podDetail.annotations).map(([key, value]) => (
                    <Descriptions.Item key={key} label={<Text code style={{ fontSize: 11 }}>{key}</Text>}>
                      <Text code style={{ fontSize: 11 }} ellipsis>{value}</Text>
                    </Descriptions.Item>
                  ))}
                </Descriptions>
              </Card>
            )}

            {/* Owner References */}
            {podDetail.ownerReferences && podDetail.ownerReferences.length > 0 && (
              <Card title="Owner References" size="small">
                <Table
                  dataSource={podDetail.ownerReferences}
                  pagination={false}
                  size="small"
                  rowKey="uid"
                  columns={[
                    {
                      title: 'Kind',
                      dataIndex: 'kind',
                      key: 'kind',
                      render: (kind: string) => <Tag color="blue">{kind}</Tag>,
                    },
                    {
                      title: 'Name',
                      dataIndex: 'name',
                      key: 'name',
                    },
                    {
                      title: 'UID',
                      dataIndex: 'uid',
                      key: 'uid',
                      render: (uid: string) => <Text code style={{ fontSize: 11 }}>{uid}</Text>,
                    },
                  ]}
                />
              </Card>
            )}

            {/* Containers */}
            <Card title={`Containers (${podDetail.containers?.length || 0})`} size="small">
              {podDetail.containers && podDetail.containers.length > 0 ? (
                <Table
                  dataSource={podDetail.containers}
                  pagination={false}
                  size="small"
                  rowKey="name"
                  columns={[
                    {
                      title: 'Name',
                      dataIndex: 'name',
                      key: 'name',
                      render: (name: string) => <Text strong>{name}</Text>,
                    },
                    {
                      title: 'Image',
                      dataIndex: 'image',
                      key: 'image',
                      ellipsis: true,
                    },
                    {
                      title: 'State',
                      dataIndex: 'state',
                      key: 'state',
                      render: (state: string, record: any) => (
                        <Space direction="vertical" size={0}>
                          <Tag color={
                            state === 'Running' ? 'green' :
                            state === 'Waiting' ? 'orange' :
                            state === 'Terminated' ? 'red' : 'default'
                          }>
                            {state}
                          </Tag>
                          {record.reason && <Text type="secondary" style={{ fontSize: 11 }}>{record.reason}</Text>}
                        </Space>
                      ),
                    },
                    {
                      title: 'Ready',
                      dataIndex: 'ready',
                      key: 'ready',
                      render: (ready: boolean) => ready ? (
                        <CheckCircleOutlined style={{ color: '#52c41a' }} />
                      ) : (
                        <CloseCircleOutlined style={{ color: '#ff4d4f' }} />
                      ),
                    },
                    {
                      title: 'Restarts',
                      dataIndex: 'restartCount',
                      key: 'restartCount',
                    },
                  ]}
                  expandable={{
                    expandedRowRender: (container: any) => (
                      <Space direction="vertical" size="small" style={{ width: '100%' }}>
                        <Descriptions bordered column={2} size="small">
                          {container.startedAt && (
                            <Descriptions.Item label="Started At">
                              {new Date(container.startedAt).toLocaleString()}
                            </Descriptions.Item>
                          )}
                          {container.finishedAt && (
                            <Descriptions.Item label="Finished At">
                              {new Date(container.finishedAt).toLocaleString()}
                            </Descriptions.Item>
                          )}
                          {container.exitCode !== undefined && (
                            <Descriptions.Item label="Exit Code">{container.exitCode}</Descriptions.Item>
                          )}
                          {container.message && (
                            <Descriptions.Item label="Message" span={2}>
                              <Text type="secondary">{container.message}</Text>
                            </Descriptions.Item>
                          )}
                        </Descriptions>

                        {/* Resources */}
                        {container.resources && (
                          <div>
                            <Text strong>Resources:</Text>
                            <Descriptions bordered column={2} size="small" style={{ marginTop: 8 }}>
                              {container.resources.requests && (
                                <Descriptions.Item label="Requests" span={2}>
                                  <Space wrap>
                                    {Object.entries(container.resources.requests).map(([key, value]) => (
                                      <Tag key={key} color="blue">
                                        {key}: {String(value)}
                                      </Tag>
                                    ))}
                                  </Space>
                                </Descriptions.Item>
                              )}
                              {container.resources.limits && (
                                <Descriptions.Item label="Limits" span={2}>
                                  <Space wrap>
                                    {Object.entries(container.resources.limits).map(([key, value]) => (
                                      <Tag key={key} color="orange">
                                        {key}: {String(value)}
                                      </Tag>
                                    ))}
                                  </Space>
                                </Descriptions.Item>
                              )}
                            </Descriptions>
                          </div>
                        )}

                        {/* Ports */}
                        {container.ports && container.ports.length > 0 && (
                          <div>
                            <Text strong>Ports:</Text>
                            <div style={{ marginTop: 8 }}>
                              <Space wrap>
                                {container.ports.map((port: any, idx: number) => (
                                  <Tag key={idx} color="green">
                                    {port.name || 'unnamed'}: {port.containerPort}/{port.protocol}
                                  </Tag>
                                ))}
                              </Space>
                            </div>
                          </div>
                        )}

                        {/* Environment Variables */}
                        {container.env && container.env.length > 0 && (
                          <div>
                            <Text strong>Environment Variables ({container.env.length}):</Text>
                            <Table
                              dataSource={container.env}
                              pagination={false}
                              size="small"
                              style={{ marginTop: 8 }}
                              columns={[
                                {
                                  title: 'Name',
                                  dataIndex: 'name',
                                  key: 'name',
                                  width: '40%',
                                  render: (name: string) => <Text code>{name}</Text>,
                                },
                                {
                                  title: 'Value',
                                  dataIndex: 'value',
                                  key: 'value',
                                  ellipsis: true,
                                  render: (value: string) => <Text code>{value}</Text>,
                                },
                              ]}
                            />
                          </div>
                        )}
                      </Space>
                    ),
                  }}
                />
              ) : (
                <Empty description="No containers" />
              )}
            </Card>

            {/* Conditions */}
            <Card title="Conditions" size="small">
              {podDetail.conditions && podDetail.conditions.length > 0 ? (
                <Table
                  dataSource={podDetail.conditions}
                  pagination={false}
                  size="small"
                  rowKey="type"
                  columns={[
                    {
                      title: 'Type',
                      dataIndex: 'type',
                      key: 'type',
                    },
                    {
                      title: 'Status',
                      dataIndex: 'status',
                      key: 'status',
                      render: (status: string) => (
                        <Tag color={status === 'True' ? 'green' : status === 'False' ? 'red' : 'default'}>
                          {status}
                        </Tag>
                      ),
                    },
                    {
                      title: 'Reason',
                      dataIndex: 'reason',
                      key: 'reason',
                      render: (reason?: string) => reason || '-',
                    },
                    {
                      title: 'Message',
                      dataIndex: 'message',
                      key: 'message',
                      ellipsis: true,
                      render: (message?: string) => message || '-',
                    },
                    {
                      title: 'Last Transition',
                      dataIndex: 'lastTransitionTime',
                      key: 'lastTransitionTime',
                      render: (time?: string) => time ? new Date(time).toLocaleString() : '-',
                    },
                  ]}
                />
              ) : (
                <Empty description="No conditions" />
              )}
            </Card>

            {/* Events */}
            <Card title={`Events (${podDetail.events?.length || 0})`} size="small">
              {podDetail.events && podDetail.events.length > 0 ? (
                <Timeline
                  items={podDetail.events.slice(0, 20).map((event) => ({
                    color: event.type === 'Warning' ? 'red' : 'blue',
                    children: (
                      <Space direction="vertical" size={0}>
                        <Space>
                          <Tag color={event.type === 'Warning' ? 'red' : 'blue'}>{event.type}</Tag>
                          <Text strong>{event.reason}</Text>
                          {event.count > 1 && <Text type="secondary">(x{event.count})</Text>}
                        </Space>
                        <Text>{event.message}</Text>
                        <Text type="secondary" style={{ fontSize: 11 }}>
                          Last seen: {new Date(event.lastSeen).toLocaleString()}
                        </Text>
                      </Space>
                    ),
                  }))}
                />
              ) : (
                <Empty description="No events" />
              )}
            </Card>

            {/* Tolerations */}
            {podDetail.tolerations && podDetail.tolerations.length > 0 && (
              <Card title={`Tolerations (${podDetail.tolerations.length})`} size="small">
                <Table
                  dataSource={podDetail.tolerations}
                  pagination={false}
                  size="small"
                  rowKey={(record, idx) => `${record.key}-${idx}`}
                  columns={[
                    {
                      title: 'Key',
                      dataIndex: 'key',
                      key: 'key',
                      render: (key: string) => <Text code>{key}</Text>,
                    },
                    {
                      title: 'Operator',
                      dataIndex: 'operator',
                      key: 'operator',
                      render: (op: string) => <Tag color="blue">{op}</Tag>,
                    },
                    {
                      title: 'Value',
                      dataIndex: 'value',
                      key: 'value',
                      render: (value?: string) => value ? <Text code>{value}</Text> : '-',
                    },
                    {
                      title: 'Effect',
                      dataIndex: 'effect',
                      key: 'effect',
                      render: (effect: string) => <Tag color="orange">{effect}</Tag>,
                    },
                  ]}
                />
              </Card>
            )}

            {/* Volumes */}
            {podDetail.volumes && podDetail.volumes.length > 0 && (
              <Card title={`Volumes (${podDetail.volumes.length})`} size="small">
                <Table
                  dataSource={podDetail.volumes}
                  pagination={false}
                  size="small"
                  rowKey="name"
                  columns={[
                    {
                      title: 'Name',
                      dataIndex: 'name',
                      key: 'name',
                    },
                    {
                      title: 'Type',
                      dataIndex: 'type',
                      key: 'type',
                      render: (type: string) => <Tag>{type}</Tag>,
                    },
                    {
                      title: 'Source',
                      dataIndex: 'source',
                      key: 'source',
                      render: (source?: Record<string, any>) =>
                        source ? <Text code>{JSON.stringify(source)}</Text> : '-',
                      ellipsis: true,
                    },
                  ]}
                />
              </Card>
            )}
          </Space>
                ),
              },
              {
                key: 'yaml',
                label: <Space><FileTextOutlined /> YAML</Space>,
                children: (
                  <div style={{ height: 'calc(100vh - 200px)', overflow: 'auto' }}>
                    <pre style={{
                      backgroundColor: '#f5f5f5',
                      padding: 16,
                      borderRadius: 4,
                      fontSize: 12,
                      fontFamily: 'Monaco, Consolas, monospace',
                      whiteSpace: 'pre-wrap',
                      wordBreak: 'break-word'
                    }}>
                      {JSON.stringify(podDetail, null, 2)}
                    </pre>
                  </div>
                ),
              },
            ]}
          />
        )}
      </Drawer>
    </>
  );
};

export default AppsPage;
