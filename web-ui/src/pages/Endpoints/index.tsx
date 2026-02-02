import { useState, useEffect } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import {
  Input, Select, Progress, Popconfirm, message, Card, Row, Col,
  Statistic, Badge, Switch, Button, Tooltip, Space, Modal, Form, InputNumber, Divider
} from 'antd';
import {
  SearchOutlined, ThunderboltOutlined, ReloadOutlined, PlusOutlined,
  DeleteOutlined, PauseCircleOutlined, CloudOutlined, SettingOutlined,
} from '@ant-design/icons';
import { api } from '@/api/client';

interface ClusterResources {
  total: { gpuCount: number; cpuCores: number; memoryGB: number };
  used: { gpuCount: number; cpuCores: number; memoryGB: number };
}

interface ClusterStatus {
  enabled: boolean;
  lastRunTime: string;
  clusterResources: ClusterResources;
}

interface GlobalConfig {
  interval: number;
  maxGpuCount: number;
  maxCpuCores: number;
  maxMemoryGB: number;
  starvationTime: number;
}

const EndpointsPage = () => {
  const queryClient = useQueryClient();
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState('all');
  const [configModalVisible, setConfigModalVisible] = useState(false);
  const [form] = Form.useForm();

  const { data: endpoints, isLoading, refetch } = useQuery({
    queryKey: ['endpoints'],
    queryFn: async () => {
      const res = await api.endpoints.list();
      return Array.isArray(res.data) ? res.data : (res.data?.endpoints || []);
    },
    refetchInterval: 10000,
  });

  const { data: clusterStatus } = useQuery<ClusterStatus>({
    queryKey: ['cluster-resources'],
    queryFn: async () => {
      const res = await api.autoscaler.getClusterResources();
      return res.data;
    },
    refetchInterval: 15000,
  });

  const { data: globalConfig } = useQuery<GlobalConfig>({
    queryKey: ['autoscaler-config'],
    queryFn: async () => {
      const res = await api.autoscaler.getGlobalConfig();
      return res.data;
    },
    enabled: configModalVisible,
  });

  useEffect(() => {
    if (globalConfig && configModalVisible) {
      form.setFieldsValue(globalConfig);
    }
  }, [globalConfig, configModalVisible, form]);

  const toggleAutoscalerMutation = useMutation({
    mutationFn: async (enabled: boolean) => {
      if (enabled) await api.autoscaler.enable();
      else await api.autoscaler.disable();
    },
    onSuccess: (_, enabled) => {
      message.success(`AutoScaler ${enabled ? 'enabled' : 'disabled'}`);
      queryClient.invalidateQueries({ queryKey: ['cluster-resources'] });
    },
    onError: () => message.error('Failed to toggle autoscaler'),
  });

  const triggerMutation = useMutation({
    mutationFn: () => api.autoscaler.trigger(),
    onSuccess: () => {
      message.success('AutoScaler triggered');
      queryClient.invalidateQueries({ queryKey: ['endpoints'] });
    },
    onError: () => message.error('Failed to trigger autoscaler'),
  });

  const updateConfigMutation = useMutation({
    mutationFn: (config: GlobalConfig) => api.autoscaler.updateGlobalConfig(config),
    onSuccess: () => {
      message.success('Configuration updated');
      setConfigModalVisible(false);
      queryClient.invalidateQueries({ queryKey: ['autoscaler-config'] });
    },
    onError: () => message.error('Failed to update configuration'),
  });

  const stopMutation = useMutation({
    mutationFn: (name: string) => api.endpoints.update(name, { endpoint: name, replicas: 0 }),
    onSuccess: () => {
      message.success('Endpoint stopped');
      queryClient.invalidateQueries({ queryKey: ['endpoints'] });
    },
    onError: () => message.error('Failed to stop endpoint'),
  });

  const deleteMutation = useMutation({
    mutationFn: (name: string) => api.endpoints.delete(name),
    onSuccess: () => {
      message.success('Endpoint deleted');
      queryClient.invalidateQueries({ queryKey: ['endpoints'] });
    },
    onError: () => message.error('Failed to delete endpoint'),
  });

  const filteredEndpoints = endpoints?.filter((ep: any) => {
    const matchSearch = ep.name.toLowerCase().includes(search.toLowerCase());
    const matchStatus = statusFilter === 'all' || ep.status === statusFilter;
    return matchSearch && matchStatus;
  }) || [];

  const getStatusClass = (status: string) => {
    switch (status) {
      case 'Running': return 'running';
      case 'Pending': return 'pending';
      case 'Failed': return 'failed';
      default: return 'stopped';
    }
  };

  const getHealthStatusClass = (healthStatus: string) => {
    switch (healthStatus) {
      case 'HEALTHY': return 'success';
      case 'DEGRADED': return 'pending';
      case 'UNHEALTHY': return 'failed';
      default: return '';
    }
  };

  const getHealthStatusIcon = (healthStatus: string) => {
    switch (healthStatus) {
      case 'HEALTHY': return '✓';
      case 'DEGRADED': return '⚠';
      case 'UNHEALTHY': return '✗';
      default: return '';
    }
  };

  const renderClusterResources = () => {
    if (!clusterStatus?.clusterResources) return null;
    const { clusterResources } = clusterStatus;
    const gpuPct = clusterResources.total.gpuCount > 0
      ? (clusterResources.used.gpuCount / clusterResources.total.gpuCount) * 100 : 0;
    const cpuPct = clusterResources.total.cpuCores > 0
      ? (clusterResources.used.cpuCores / clusterResources.total.cpuCores) * 100 : 0;
    const memPct = clusterResources.total.memoryGB > 0
      ? (clusterResources.used.memoryGB / clusterResources.total.memoryGB) * 100 : 0;

    return (
      <Card size="small" style={{ marginBottom: 16 }}>
        <Row justify="space-between" align="middle" style={{ marginBottom: 12 }}>
          <Col>
            <Space>
              <CloudOutlined style={{ fontSize: 18, color: '#1890ff' }} />
              <span style={{ fontWeight: 600, fontSize: 15 }}>Cluster Resources</span>
              <Badge
                status={clusterStatus.enabled ? 'success' : 'default'}
                text={clusterStatus.enabled ? 'Enabled' : 'Disabled'}
              />
            </Space>
          </Col>
          <Col>
            <Space>
              <Switch
                size="small"
                checked={clusterStatus.enabled}
                loading={toggleAutoscalerMutation.isPending}
                onChange={(checked) => toggleAutoscalerMutation.mutate(checked)}
              />
              <Tooltip title="Trigger AutoScaler">
                <Button
                  size="small"
                  icon={<ThunderboltOutlined />}
                  onClick={() => triggerMutation.mutate()}
                  loading={triggerMutation.isPending}
                >
                  Trigger
                </Button>
              </Tooltip>
              <Button size="small" icon={<SettingOutlined />} onClick={() => setConfigModalVisible(true)}>
                Config
              </Button>
            </Space>
          </Col>
        </Row>
        <Row gutter={24}>
          <Col span={8}>
            <Statistic
              title="GPU"
              value={clusterResources.used.gpuCount}
              suffix={`/ ${clusterResources.total.gpuCount}`}
              valueStyle={{ fontSize: 18 }}
            />
            <Progress percent={Number(gpuPct.toFixed(0))} size="small" status="active" />
          </Col>
          <Col span={8}>
            <Statistic
              title="CPU Cores"
              value={clusterResources.used.cpuCores.toFixed(1)}
              suffix={`/ ${clusterResources.total.cpuCores || '∞'}`}
              valueStyle={{ fontSize: 18 }}
            />
            <Progress percent={Number(cpuPct.toFixed(0))} size="small" status="active" />
          </Col>
          <Col span={8}>
            <Statistic
              title="Memory (GB)"
              value={clusterResources.used.memoryGB.toFixed(1)}
              suffix={`/ ${clusterResources.total.memoryGB || '∞'}`}
              valueStyle={{ fontSize: 18 }}
            />
            <Progress percent={Number(memPct.toFixed(0))} size="small" status="active" />
          </Col>
        </Row>
      </Card>
    );
  };

  if (isLoading) {
    return <div className="loading"><div className="spinner"></div></div>;
  }

  return (
    <>
      <div className="flex justify-between items-center mb-4">
        <div className="filters">
          <Input
            placeholder="Search endpoints..."
            prefix={<SearchOutlined />}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            style={{ width: 240 }}
          />
          <Select
            value={statusFilter}
            onChange={setStatusFilter}
            style={{ width: 140 }}
            options={[
              { value: 'all', label: 'All Status' },
              { value: 'Running', label: 'Running' },
              { value: 'Pending', label: 'Pending' },
              { value: 'Stopped', label: 'Stopped' },
            ]}
          />
        </div>
        <div className="flex gap-2">
          <button className="btn btn-outline" onClick={() => refetch()}>
            <ReloadOutlined /> Refresh
          </button>
          <Link to="/serverless" className="btn btn-blue">
            <PlusOutlined /> New Endpoint
          </Link>
        </div>
      </div>

      {renderClusterResources()}

      <div className="card">
        <div className="table-container">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Spec</th>
                <th>Replicas</th>
                <th>Status</th>
                <th>Health</th>
                <th>Image</th>
                <th>Created</th>
                <th style={{ width: 100 }}>Actions</th>
              </tr>
            </thead>
            <tbody>
              {filteredEndpoints.map((ep: any) => (
                <tr key={ep.name}>
                  <td>
                    <Link to={`/endpoints/${ep.name}`} style={{ color: '#1da1f2', fontWeight: 500 }}>
                      {ep.name}
                    </Link>
                  </td>
                  <td>
                    <span className="tag" style={{ background: 'rgba(139,92,246,0.1)', color: '#8b5cf6' }}>
                      {ep.specName || 'N/A'}
                    </span>
                  </td>
                  <td>{ep.readyReplicas || 0} / {ep.replicas || 0}</td>
                  <td>
                    <span className={`tag ${getStatusClass(ep.status)}`}>{ep.status}</span>
                  </td>
                  <td>
                    {ep.healthStatus && (
                      <Tooltip title={ep.healthMessage || ''}>
                        <span className={`tag ${getHealthStatusClass(ep.healthStatus)}`}>
                          {getHealthStatusIcon(ep.healthStatus)} {ep.healthStatus}
                        </span>
                      </Tooltip>
                    )}
                  </td>
                  <td style={{ maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis' }}>
                    {ep.image}
                  </td>
                  <td style={{ color: '#6b7280', fontSize: 12 }}>
                    {ep.createdAt ? new Date(ep.createdAt).toLocaleDateString() : '-'}
                  </td>
                  <td>
                    <div className="flex gap-1">
                      <Popconfirm
                        title="Stop this endpoint?"
                        description="This will set replicas to 0"
                        onConfirm={() => stopMutation.mutate(ep.name)}
                        okText="Stop"
                        disabled={ep.replicas === 0}
                      >
                        <button
                          className="btn btn-sm btn-outline"
                          disabled={ep.replicas === 0}
                          title="Stop (set replicas to 0)"
                        >
                          <PauseCircleOutlined />
                        </button>
                      </Popconfirm>
                      <Popconfirm
                        title="Delete this endpoint?"
                        description="This action cannot be undone"
                        onConfirm={() => deleteMutation.mutate(ep.name)}
                        okText="Delete"
                        okButtonProps={{ danger: true }}
                      >
                        <button className="btn btn-sm btn-outline" style={{ color: '#ef4444' }} title="Delete">
                          <DeleteOutlined />
                        </button>
                      </Popconfirm>
                    </div>
                  </td>
                </tr>
              ))}
              {filteredEndpoints.length === 0 && (
                <tr>
                  <td colSpan={8}>
                    <div className="empty-state">
                      <ThunderboltOutlined style={{ fontSize: 48, opacity: 0.3 }} />
                      <p>No endpoints found</p>
                    </div>
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      <Modal
        title="AutoScaler Configuration"
        open={configModalVisible}
        onCancel={() => setConfigModalVisible(false)}
        onOk={() => form.submit()}
        confirmLoading={updateConfigMutation.isPending}
        width={500}
      >
        <Form form={form} layout="vertical" onFinish={(v) => updateConfigMutation.mutate(v)}>
          <Divider orientation="left" plain>Control Loop</Divider>
          <Form.Item name="interval" label="Interval (seconds)" rules={[{ required: true }]}>
            <InputNumber min={1} max={300} style={{ width: '100%' }} />
          </Form.Item>

          <Divider orientation="left" plain>Cluster Limits</Divider>
          <Row gutter={12}>
            <Col span={8}>
              <Form.Item name="maxGpuCount" label="Max GPU" rules={[{ required: true }]}>
                <InputNumber min={0} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item name="maxCpuCores" label="Max CPU" help="0=unlimited" rules={[{ required: true }]}>
                <InputNumber min={0} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
            <Col span={8}>
              <Form.Item name="maxMemoryGB" label="Max Mem(GB)" help="0=unlimited" rules={[{ required: true }]}>
                <InputNumber min={0} style={{ width: '100%' }} />
              </Form.Item>
            </Col>
          </Row>

          <Divider orientation="left" plain>Starvation</Divider>
          <Form.Item name="starvationTime" label="Starvation Time (seconds)" rules={[{ required: true }]}>
            <InputNumber min={0} max={3600} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
};

export default EndpointsPage;
