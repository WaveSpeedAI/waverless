import React, { useState, useEffect, useRef, useMemo, useCallback } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { message, Popconfirm, Modal, Form, Input, InputNumber, Select, Switch, Collapse, Drawer, Tabs, Tooltip, Timeline, Button } from 'antd';
import {
  ArrowLeftOutlined, DeleteOutlined, ReloadOutlined, EditOutlined,
  PlayCircleOutlined, CopyOutlined, PlusOutlined, SyncOutlined,
  SearchOutlined, EyeOutlined, StopOutlined, QuestionCircleOutlined,
} from '@ant-design/icons';
import * as echarts from 'echarts';
import { api } from '@/api/client';
import { AppInfo, WorkerWithPodInfo, UpdateEndpointConfigRequest, UpdateDeploymentRequest, SpecInfo, PVCInfo, Task } from '@/types';

type TabKey = 'overview' | 'metrics' | 'workers' | 'tasks' | 'scaling' | 'settings';

const FIELD_TIPS: Record<string, string> = {
  priority: 'Higher numbers get resources first. Default 50.',
  scaleUpThreshold: 'Pending tasks required before adding replicas (>=1).',
  scaleDownIdleTime: 'Idle seconds before replicas are removed.',
  scaleUpCooldown: 'Minimum seconds between two scale-up actions.',
  scaleDownCooldown: 'Minimum seconds between two scale-down actions.',
  highLoadThreshold: 'Queue length treated as "high load" for dynamic priority.',
  priorityBoost: 'Priority points to add when high load is detected.',
  imagePrefix: 'Registry prefix prepended to image name.',
};

const TipIcon = ({ tip }: { tip: string }) => (
  <Tooltip title={tip}><QuestionCircleOutlined style={{ marginLeft: 4, color: '#999', fontSize: 12 }} /></Tooltip>
);

const EndpointDetailPage = () => {
  const { name } = useParams<{ name: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [activeTab, setActiveTab] = useState<TabKey>('overview');
  const [apiMethod, setApiMethod] = useState<'run' | 'runsync' | 'status'>('run');
  const [codeLang, setCodeLang] = useState<'curl' | 'python' | 'js'>('curl');
  const [inputJson, setInputJson] = useState('{"prompt": "a beautiful sunset"}');
  const [taskIdInput, setTaskIdInput] = useState('');
  const [response, setResponse] = useState<any>(null);
  const [editModalOpen, setEditModalOpen] = useState(false);
  const [form] = Form.useForm();

  const { data: endpoint, isLoading, refetch } = useQuery({
    queryKey: ['endpoint', name],
    queryFn: async () => (await api.endpoints.get(name!)).data,
    enabled: !!name,
  });

  const { data: taskStats } = useQuery({
    queryKey: ['endpoint-task-stats', name],
    queryFn: async () => (await api.get(`/api/v1/statistics/endpoints/${name}`)).data,
    enabled: !!name,
    refetchInterval: 1000,
  });

  const { data: workers } = useQuery({
    queryKey: ['endpoint-workers', name],
    queryFn: async () => {
      const res = await api.endpoints.getWorkers(name!);
      return Array.isArray(res.data) ? res.data : (res.data?.workers || []);
    },
    enabled: !!name,
    refetchInterval: 5000,
  });

  const deleteMutation = useMutation({
    mutationFn: () => api.endpoints.delete(name!),
    onSuccess: () => { message.success('Endpoint deleted'); navigate('/endpoints'); },
    onError: (e: any) => message.error(e.response?.data?.error || 'Failed'),
  });

  const checkImageMutation = useMutation({
    mutationFn: () => api.apps.checkImage(name!),
    onSuccess: (res) => { message.success(res.data.updateAvailable ? 'Update available!' : 'Image is up to date'); refetch(); },
    onError: (e: any) => message.error(e.response?.data?.error || 'Failed to check image'),
  });

  const updateMutation = useMutation({
    mutationFn: (data: UpdateDeploymentRequest) => api.endpoints.updateDeployment(name!, data),
    onSuccess: () => { message.success('Updated'); setEditModalOpen(false); queryClient.invalidateQueries({ queryKey: ['endpoint', name] }); },
    onError: (e: any) => message.error(e.response?.data?.error || 'Failed'),
  });

  const getStatusClass = (s: string) => s === 'Running' ? 'running' : s === 'Pending' ? 'pending' : s === 'Failed' ? 'failed' : 'stopped';

  const copyCode = () => {
    navigator.clipboard.writeText(getCodeExample());
    message.success('Copied!');
  };

  const getCodeExample = () => {
    const base = window.location.origin;
    if (codeLang === 'curl') {
      if (apiMethod === 'status') return `curl -X GET ${base}/v1/status/${taskIdInput || 'TASK_ID'}`;
      return `curl -X POST ${base}/v1/${name}/${apiMethod} \\\n  -H "Content-Type: application/json" \\\n  -d '{"input": ${inputJson}}'`;
    }
    if (codeLang === 'python') {
      if (apiMethod === 'status') return `import requests\n\nres = requests.get("${base}/v1/status/${taskIdInput || 'TASK_ID'}")\nprint(res.json())`;
      return `import requests\n\nres = requests.post(\n    "${base}/v1/${name}/${apiMethod}",\n    json={"input": ${inputJson}}\n)\nprint(res.json())`;
    }
    if (apiMethod === 'status') return `const res = await fetch("${base}/v1/status/${taskIdInput || 'TASK_ID'}");\nconsole.log(await res.json());`;
    return `const res = await fetch("${base}/v1/${name}/${apiMethod}", {\n  method: "POST",\n  headers: { "Content-Type": "application/json" },\n  body: JSON.stringify({ input: ${inputJson} })\n});\nconsole.log(await res.json());`;
  };

  const submitTask = async () => {
    try {
      const input = JSON.parse(inputJson);
      const res = apiMethod === 'runsync'
        ? await api.tasks.submitSync(name!, input)
        : await api.tasks.submit(name!, input);
      setResponse(res.data);
      if (res.data?.id) setTaskIdInput(res.data.id);
    } catch (e: any) {
      message.error(e.message || 'Failed');
    }
  };

  const queryStatus = async () => {
    try {
      const res = await api.tasks.get(taskIdInput);
      setResponse(res.data);
    } catch (e: any) {
      message.error(e.message || 'Failed');
    }
  };

  const openEditModal = () => {
    if (!endpoint) return;
    form.setFieldsValue({
      replicas: endpoint.replicas,
      image: endpoint.image,
    });
    setEditModalOpen(true);
  };

  if (isLoading) return <div className="loading"><div className="spinner"></div></div>;
  if (!endpoint) return <div className="empty-state"><p>Endpoint not found</p><Link to="/endpoints" className="btn btn-outline mt-4">Back</Link></div>;

  const ep = endpoint as AppInfo;

  return (
    <>
      {/* Header */}
      <div className="flex justify-between items-center mb-4">
        <div className="flex items-center gap-3">
          <Link to="/endpoints" className="btn btn-outline btn-icon"><ArrowLeftOutlined /></Link>
          <h2 style={{ margin: 0, fontSize: 20, fontWeight: 600 }}>{name}</h2>
          <span className={`tag ${getStatusClass(ep.status)}`}>{ep.status}</span>
        </div>
        <div className="flex gap-2">
          <button className="btn btn-outline" onClick={() => refetch()}><ReloadOutlined /> Refresh</button>
          <button className="btn btn-outline" onClick={openEditModal}><EditOutlined /> Edit</button>
          <Popconfirm title="Delete this endpoint?" onConfirm={() => deleteMutation.mutate()} okText="Yes" cancelText="No">
            <button className="btn btn-outline" style={{ color: '#f56565', borderColor: '#f56565' }}><DeleteOutlined /> Delete</button>
          </Popconfirm>
        </div>
      </div>

      {/* Meta */}
      <div style={{ fontSize: 13, color: '#6b7280', marginBottom: 12 }}>
        {ep.specName} • Created {ep.createdAt ? new Date(ep.createdAt).toLocaleDateString() : '-'}
      </div>

      {/* Overview Stats Cards */}
      <div className="stats-grid mb-4" style={{ gridTemplateColumns: 'repeat(5, 1fr)' }}>
        <div className="stat-card">
          <div className="stat-content">
            <div className="stat-label">Workers</div>
            <div className="stat-value" style={{ color: '#3b82f6' }}>
              <span style={{ fontSize: 28 }}>{taskStats?.busyWorkers || 0}</span>
              <span style={{ fontSize: 14, color: '#6b7280' }}> / {taskStats?.onlineWorkers || 0}</span>
            </div>
          </div>
        </div>
        <div className="stat-card">
          <div className="stat-content">
            <div className="stat-label">Replicas</div>
            <div className="stat-value" style={{ color: '#10b981' }}>
              <span style={{ fontSize: 28 }}>{ep.readyReplicas || 0}</span>
              <span style={{ fontSize: 14, color: '#6b7280' }}> / {ep.replicas || 0}</span>
            </div>
          </div>
        </div>
        <div className="stat-card">
          <div className="stat-content">
            <div className="stat-label">Running</div>
            <div className="stat-value" style={{ color: '#3b82f6', fontSize: 28 }}>{taskStats?.runningTasks || 0}</div>
          </div>
        </div>
        <div className="stat-card">
          <div className="stat-content">
            <div className="stat-label">Pending</div>
            <div className="stat-value" style={{ color: '#f59e0b', fontSize: 28 }}>{taskStats?.pendingTasks || 0}</div>
          </div>
        </div>
        <div className="stat-card">
          <div className="stat-content">
            <div className="stat-label">Total</div>
            <div className="stat-value" style={{ fontSize: 28 }}>{(taskStats?.pendingTasks || 0) + (taskStats?.runningTasks || 0) + (taskStats?.completedTasks || 0) + (taskStats?.failedTasks || 0)}</div>
          </div>
        </div>
      </div>

      {/* Tabs */}
      <div className="tabs mb-4">
        {(['overview', 'metrics', 'workers', 'tasks', 'scaling', 'settings'] as TabKey[]).map(t => (
          <div key={t} className={`tab ${activeTab === t ? 'active' : ''}`} onClick={() => setActiveTab(t)}>
            {t === 'scaling' ? 'Scaling History' : t.charAt(0).toUpperCase() + t.slice(1)}
          </div>
        ))}
      </div>

      {/* Overview Tab */}
      {activeTab === 'overview' && (
        <>
          {/* Quick Start - Collapsible */}
          <Collapse defaultActiveKey={['quickstart']} className="mb-4" items={[{
            key: 'quickstart',
            label: '⚡ Quick Start',
            children: (
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 20 }}>
                {/* Left: Submit Task */}
                <div>
                  <div className="flex gap-2 mb-3">
                    {(['run', 'runsync', 'status'] as const).map(m => (
                      <button key={m} className={`btn btn-sm ${apiMethod === m ? 'btn-blue' : 'btn-outline'}`} onClick={() => setApiMethod(m)}>
                        {m === 'run' ? 'Async' : m === 'runsync' ? 'Sync' : 'Status'}
                      </button>
                    ))}
                  </div>
                  {apiMethod !== 'status' ? (
                    <div className="form-group mb-3">
                      <label className="form-label">Input JSON</label>
                      <textarea className="form-input" rows={4} value={inputJson} onChange={e => setInputJson(e.target.value)} style={{ fontFamily: 'monospace', fontSize: 12 }} />
                    </div>
                  ) : (
                    <div className="form-group mb-3">
                      <label className="form-label">Task ID</label>
                      <Input value={taskIdInput} onChange={e => setTaskIdInput(e.target.value)} placeholder="Enter task ID" />
                    </div>
                  )}
                  <button className="btn btn-blue" onClick={apiMethod === 'status' ? queryStatus : submitTask}>
                    <PlayCircleOutlined /> {apiMethod === 'status' ? 'Query' : 'Submit'}
                  </button>
                  {response && (
                    <div style={{ marginTop: 12 }}>
                      <label className="form-label">Response</label>
                      <pre style={{ background: '#1f2937', color: '#e5e7eb', padding: 10, borderRadius: 6, fontSize: 11, overflow: 'auto', maxHeight: 120 }}>
                        {JSON.stringify(response, null, 2)}
                      </pre>
                    </div>
                  )}
                </div>
                {/* Right: Code Example */}
                <div>
                  <div className="flex gap-2 mb-2">
                    {(['curl', 'python', 'js'] as const).map(l => (
                      <span key={l} className={`code-tab ${codeLang === l ? 'active' : ''}`} onClick={() => setCodeLang(l)}>{l}</span>
                    ))}
                    <button className="btn btn-outline btn-sm" style={{ marginLeft: 'auto', fontSize: 11 }} onClick={copyCode}><CopyOutlined /> Copy</button>
                  </div>
                  <pre style={{ background: '#1f2937', color: '#e5e7eb', padding: 12, borderRadius: 6, fontSize: 11, overflow: 'auto', height: 180 }}>{getCodeExample()}</pre>
                </div>
              </div>
            ),
          }]} />

          {/* Basic Info */}
          <div className="card mb-4">
            <div className="card-header"><h3>Basic Information</h3></div>
            <table className="info-table">
              <tbody>
                <tr><td className="info-label">Endpoint Name</td><td colSpan={3}>{ep.name}</td></tr>
                {ep.displayName && <tr><td className="info-label">Display Name</td><td colSpan={3}>{ep.displayName}</td></tr>}
                {ep.description && <tr><td className="info-label">Description</td><td colSpan={3}>{ep.description}</td></tr>}
                <tr><td className="info-label">Namespace</td><td>{ep.namespace || 'default'}</td><td className="info-label">Type</td><td>{ep.type}</td></tr>
                <tr><td className="info-label">Spec</td><td>{ep.specName || 'N/A'}</td><td className="info-label">Task Timeout</td><td>{ep.taskTimeout || 3600}s</td></tr>
                <tr><td className="info-label">Max Pending Tasks</td><td>{ep.maxPendingTasks || 100}</td><td className="info-label">Created At</td><td>{ep.createdAt ? new Date(ep.createdAt).toLocaleString() : '-'}</td></tr>
                {ep.healthStatus && (
                  <tr>
                    <td className="info-label">Health Status</td>
                    <td colSpan={3}>
                      <span className={`tag ${ep.healthStatus === 'HEALTHY' ? 'success' : ep.healthStatus === 'DEGRADED' ? 'pending' : 'failed'}`}>
                        {ep.healthStatus === 'HEALTHY' ? '✓' : ep.healthStatus === 'DEGRADED' ? '⚠' : '✗'} {ep.healthStatus}
                      </span>
                      {ep.healthMessage && <span style={{ marginLeft: 8, color: '#6b7280', fontSize: 12 }}>{ep.healthMessage}</span>}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>

          {/* Image Info */}
          <div className="card mb-4">
            <div className="card-header">
              <h3>Image Information</h3>
              <button className="btn btn-outline btn-sm" onClick={() => checkImageMutation.mutate()} disabled={checkImageMutation.isPending}>
                <SyncOutlined spin={checkImageMutation.isPending} /> Check Image
              </button>
            </div>
            <table className="info-table">
              <tbody>
                {ep.imagePrefix && <tr><td className="info-label">Image Prefix<TipIcon tip={FIELD_TIPS.imagePrefix} /></td><td colSpan={3}><code style={{ background: '#f3f4f6', padding: '4px 8px', borderRadius: 4, fontSize: 11 }}>{ep.imagePrefix}</code></td></tr>}
                <tr><td className="info-label">Image</td><td colSpan={3}><code style={{ background: '#f3f4f6', padding: '4px 8px', borderRadius: 4, fontSize: 11 }}>{ep.image}</code></td></tr>
                <tr>
                  <td className="info-label">Update Status</td>
                  <td colSpan={3}>
                    <span className={`tag ${ep.imageUpdateAvailable ? 'pending' : 'success'}`}>
                      {ep.imageUpdateAvailable ? '⚠️ Update Available' : '✓ Image is up to date'}
                    </span>
                  </td>
                </tr>
                {ep.imageDigest && <tr><td className="info-label">Image Digest</td><td><code style={{ fontSize: 11 }}>{ep.imageDigest}</code></td><td className="info-label">Last Checked</td><td>{ep.imageLastChecked ? new Date(ep.imageLastChecked).toLocaleString() : '-'}</td></tr>}
              </tbody>
            </table>
          </div>

          {/* Resource Config */}
          <div className="card mb-4">
            <div className="card-header"><h3>Resource Configuration</h3></div>
            <table className="info-table">
              <tbody>
                <tr><td className="info-label">Replicas</td><td>{ep.readyReplicas || 0} / {ep.replicas || 0}</td><td className="info-label">Available</td><td>{ep.availableReplicas || 0}</td></tr>
                {ep.shmSize && <tr><td className="info-label">Shared Memory</td><td>{ep.shmSize}</td><td className="info-label">Ptrace</td><td>{ep.enablePtrace ? 'Enabled' : 'Disabled'}</td></tr>}
              </tbody>
            </table>
          </div>

          {/* AutoScaler Config */}
          <div className="card mb-4">
            <div className="card-header"><h3>AutoScaler Configuration</h3></div>
            <table className="info-table">
              <tbody>
                <tr><td className="info-label">AutoScaler</td><td colSpan={3}><span className={`tag ${ep.autoscalerEnabled === 'enabled' ? 'running' : ep.autoscalerEnabled === 'disabled' ? 'failed' : 'pending'}`}>{ep.autoscalerEnabled === 'enabled' ? 'Force On' : ep.autoscalerEnabled === 'disabled' ? 'Force Off' : 'Default'}</span></td></tr>
                <tr><td className="info-label">Priority<TipIcon tip={FIELD_TIPS.priority} /></td><td>{ep.priority || 50}</td><td className="info-label">Min Replicas</td><td>{ep.minReplicas || 0}{ep.minReplicas === 0 && <span style={{ color: '#6b7280', fontSize: 11, marginLeft: 4 }}>(scale-to-zero)</span>}</td></tr>
                <tr><td className="info-label">Max Replicas</td><td>{ep.maxReplicas || 10}</td><td className="info-label">Scale Up Threshold<TipIcon tip={FIELD_TIPS.scaleUpThreshold} /></td><td>{ep.scaleUpThreshold || 1}</td></tr>
                <tr><td className="info-label">Scale Down Idle<TipIcon tip={FIELD_TIPS.scaleDownIdleTime} /></td><td>{ep.scaleDownIdleTime || 300}s</td><td className="info-label">Scale Up Cooldown<TipIcon tip={FIELD_TIPS.scaleUpCooldown} /></td><td>{ep.scaleUpCooldown || 60}s</td></tr>
                <tr><td className="info-label">Scale Down Cooldown<TipIcon tip={FIELD_TIPS.scaleDownCooldown} /></td><td>{ep.scaleDownCooldown || 120}s</td><td className="info-label">Dynamic Priority</td><td>{ep.enableDynamicPrio ? 'Enabled' : 'Disabled'}</td></tr>
                <tr><td className="info-label">High Load Threshold<TipIcon tip={FIELD_TIPS.highLoadThreshold} /></td><td>{ep.highLoadThreshold || 50}</td><td className="info-label">Priority Boost<TipIcon tip={FIELD_TIPS.priorityBoost} /></td><td>{ep.priorityBoost || 10}</td></tr>
              </tbody>
            </table>
          </div>

          {/* Task Stats */}
          <div className="card mb-4">
            <div className="card-header"><h3>Task Statistics</h3></div>
            <table className="info-table">
              <tbody>
                <tr><td className="info-label">Total Tasks</td><td><strong>{(taskStats?.pendingTasks || 0) + (taskStats?.runningTasks || 0) + (taskStats?.completedTasks || 0) + (taskStats?.failedTasks || 0)}</strong></td><td className="info-label">Completed</td><td><span className="tag success">{taskStats?.completedTasks || 0}</span></td></tr>
                <tr><td className="info-label">Failed</td><td><span className="tag failed">{taskStats?.failedTasks || 0}</span></td><td className="info-label">Pending</td><td><span className="tag pending">{taskStats?.pendingTasks || 0}</span></td></tr>
                <tr><td className="info-label">Running</td><td><span className="tag running">{taskStats?.runningTasks || 0}</span></td><td className="info-label">Workers</td><td>{taskStats?.busyWorkers || 0} / {taskStats?.onlineWorkers || 0}</td></tr>
              </tbody>
            </table>
          </div>

          {/* Volume Mounts */}
          {ep.volumeMounts && ep.volumeMounts.length > 0 && (
            <div className="card mb-4">
              <div className="card-header"><h3>Volume Mounts</h3></div>
              <table className="info-table">
                <tbody>
                  {ep.volumeMounts.map((v, i) => (
                    <tr key={i}><td className="info-label">Mount {i + 1}</td><td colSpan={3}>PVC: <code>{v.pvcName}</code> → Path: <code>{v.mountPath}</code></td></tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}

      {/* Metrics Tab */}
      {activeTab === 'metrics' && <MetricsTab endpoint={ep} />}

      {/* Workers Tab */}
      {activeTab === 'workers' && <WorkersTab workers={workers || []} endpoint={name!} />}

      {/* Tasks Tab */}
      {activeTab === 'tasks' && <TasksTab endpoint={name!} workers={workers || []} />}

      {/* Scaling History Tab */}
      {activeTab === 'scaling' && <ScalingHistoryTab endpoint={name!} />}

      {/* Settings Tab */}
      {activeTab === 'settings' && <SettingsTab endpoint={ep} name={name!} />}

      {/* Edit Modal - Quick Edit */}
      <Modal title="Quick Edit" open={editModalOpen} onCancel={() => setEditModalOpen(false)} onOk={() => form.submit()} confirmLoading={updateMutation.isPending}>
        <Form form={form} layout="vertical" onFinish={(v) => updateMutation.mutate({ endpoint: name!, replicas: v.replicas, image: v.image })}>
          <Form.Item name="replicas" label="Replicas"><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="image" label="Docker Image"><Input /></Form.Item>
        </Form>
      </Modal>
    </>
  );
};

const ChartCard = React.memo(({ chartRef, title, total, legend, hasData }: { chartRef: React.RefObject<HTMLDivElement>; title: string; total?: string; legend?: { color: string; label: string }[]; hasData: boolean }) => (
  <div className="chart-card">
    <div className="chart-header"><span className="chart-title">{title}</span>{total && <span className="chart-total">{total}</span>}</div>
    {legend && <div className="chart-legend">{legend.map((l, i) => <span key={i} className="legend-item"><span className="legend-dot" style={{ background: l.color }}></span>{l.label}</span>)}</div>}
    <div className="chart-container" ref={chartRef} style={{ display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      {!hasData && <span style={{ color: '#9ca3af', fontSize: 13 }}>No data for selected time range</span>}
    </div>
  </div>
));

const MetricsTab = ({ endpoint }: { endpoint: AppInfo }) => {
  const [timeRange, setTimeRange] = useState('1h');
  const [liveMode, setLiveMode] = useState(true);
  const [showRangeDropdown, setShowRangeDropdown] = useState(false);
  const requestsRef = useRef<HTMLDivElement>(null);
  const executionRef = useRef<HTMLDivElement>(null);
  const delayRef = useRef<HTMLDivElement>(null);
  const coldStartCountRef = useRef<HTMLDivElement>(null);
  const coldStartTimeRef = useRef<HTMLDivElement>(null);
  const workersRef = useRef<HTMLDivElement>(null);
  const utilizationRef = useRef<HTMLDivElement>(null);
  const idleTimeRef = useRef<HTMLDivElement>(null);

  const rangeMs: Record<string, number> = { '1h': 3600000, '6h': 6 * 3600000, '24h': 24 * 3600000, '7d': 7 * 24 * 3600000, '30d': 30 * 24 * 3600000 };
  const rangeLabels: Record<string, string> = { '1h': 'Last 1 hour', '6h': 'Last 6 hours', '24h': 'Last 24 hours', '7d': 'Last 7 days', '30d': 'Last 30 days' };

  const { data: realtimeData } = useQuery({
    queryKey: ['endpoint-realtime', endpoint.name],
    queryFn: async () => (await api.get(`/v1/${endpoint.name}/metrics/realtime`)).data,
    refetchInterval: liveMode ? 5000 : false,
  });

  const { data: statsResponse } = useQuery({
    queryKey: ['endpoint-stats', endpoint.name, timeRange, liveMode ? Math.floor(Date.now() / 60000) : 'static'],
    queryFn: async () => {
      const [from, to] = [new Date(Date.now() - rangeMs[timeRange]), new Date()];
      const res = await api.get(`/v1/${endpoint.name}/metrics/stats`, { params: { from: from.toISOString(), to: to.toISOString() } });
      return res.data;
    },
    placeholderData: (prev) => prev,
    refetchInterval: liveMode ? 60000 : false,
  });

  const statsData = statsResponse?.stats || [];
  const granularity = statsResponse?.granularity || '1h';

  const grid = useMemo(() => ({ left: 50, right: 20, top: 20, bottom: 50 }), []);
  const axisLabel = useMemo(() => ({ fontSize: 11, color: '#6b7280' }), []);
  const dataZoom = useMemo(() => [{ type: 'inside', start: 0, end: 100 }, { type: 'slider', start: 0, end: 100, height: 20, bottom: 5 }], []);
  
  const { totals, avgValues } = useMemo(() => {
    const sum = (key: string) => statsData?.reduce((a: number, s: any) => a + (s[key] || 0), 0) || 0;
    const avg = (key: string) => statsData?.length ? (sum(key) / statsData.length).toFixed(1) : '0';
    return {
      totals: { submitted: sum('tasks_submitted'), completed: sum('tasks_completed'), failed: sum('tasks_failed'), retried: sum('tasks_retried'), coldStarts: sum('cold_starts') },
      avgValues: { activeWorkers: avg('active_workers'), workerUtilization: avg('avg_worker_utilization') }
    };
  }, [statsData]);

  const formatLabel = useCallback((ts: string) => {
    const d = new Date(ts);
    return granularity === '1d' ? d.toLocaleDateString('en', { month: 'short', day: 'numeric' }) : d.toLocaleTimeString('en', { hour: '2-digit', minute: '2-digit' });
  }, [granularity]);

  useEffect(() => {
    if (!statsData?.length) return;
    const labels = statsData.map((s: any) => formatLabel(s.timestamp));
    const init = (ref: React.RefObject<HTMLDivElement>, opt: echarts.EChartsOption) => {
      if (!ref.current) return;
      let c = echarts.getInstanceByDom(ref.current);
      if (!c) c = echarts.init(ref.current);
      c.setOption({ ...opt, dataZoom }, true);
    };

    init(requestsRef, { grid, tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } }, xAxis: { type: 'category', data: labels, axisLabel }, yAxis: { type: 'value', axisLabel }, series: [
      { name: 'Submitted', type: 'bar', stack: 'total', data: statsData.map((s: any) => s.tasks_submitted || 0), itemStyle: { color: '#3b82f6' }, barMaxWidth: 10 },
      { name: 'Completed', type: 'bar', stack: 'total', data: statsData.map((s: any) => s.tasks_completed || 0), itemStyle: { color: '#48bb78' }, barMaxWidth: 10 },
      { name: 'Failed', type: 'bar', stack: 'total', data: statsData.map((s: any) => s.tasks_failed || 0), itemStyle: { color: '#f56565' }, barMaxWidth: 10 },
      { name: 'Retried', type: 'bar', stack: 'total', data: statsData.map((s: any) => s.tasks_retried || 0), itemStyle: { color: '#ecc94b' }, barMaxWidth: 10 },
    ]});
    init(executionRef, { grid, tooltip: { trigger: 'axis' }, xAxis: { type: 'category', data: labels, axisLabel }, yAxis: { type: 'value', axisLabel: { ...axisLabel, formatter: (v: number) => (v / 1000).toFixed(0) + 's' } }, series: [
      { name: 'P95', type: 'line', data: statsData.map((s: any) => s.p95_execution_ms || 0), smooth: true, symbol: 'none', lineStyle: { width: 1, color: '#f56565' }, areaStyle: { color: { type: 'linear', x: 0, y: 0, x2: 0, y2: 1, colorStops: [{ offset: 0, color: 'rgba(245,101,101,0.3)' }, { offset: 1, color: 'rgba(245,101,101,0.05)' }] } } },
      { name: 'P50', type: 'line', data: statsData.map((s: any) => s.p50_execution_ms || s.avg_execution_ms || 0), smooth: true, symbol: 'none', lineStyle: { width: 1, color: '#3b82f6' }, areaStyle: { color: { type: 'linear', x: 0, y: 0, x2: 0, y2: 1, colorStops: [{ offset: 0, color: 'rgba(59,130,246,0.3)' }, { offset: 1, color: 'rgba(59,130,246,0.05)' }] } } },
    ]});
    init(delayRef, { grid, tooltip: { trigger: 'axis' }, xAxis: { type: 'category', data: labels, axisLabel }, yAxis: { type: 'value', axisLabel: { ...axisLabel, formatter: (v: number) => (v / 1000).toFixed(0) + 's' } }, series: [{ name: 'Delay', type: 'bar', data: statsData.map((s: any) => s.avg_queue_wait_ms || 0), itemStyle: { color: '#f56565' }, barMaxWidth: 8 }] });
    init(coldStartCountRef, { grid, tooltip: { trigger: 'axis' }, xAxis: { type: 'category', data: labels, axisLabel }, yAxis: { type: 'value', axisLabel }, series: [{ name: 'Count', type: 'bar', data: statsData.map((s: any) => s.cold_starts || 0), itemStyle: { color: '#8b5cf6' }, barMaxWidth: 8 }] });
    init(coldStartTimeRef, { grid, tooltip: { trigger: 'axis' }, xAxis: { type: 'category', data: labels, axisLabel }, yAxis: { type: 'value', axisLabel: { ...axisLabel, formatter: (v: number) => v.toFixed(0) + 's' } }, series: [{ name: 'Time', type: 'bar', data: statsData.map((s: any) => (s.avg_cold_start_ms || 0) / 1000), itemStyle: { color: '#06b6d4' }, barMaxWidth: 8 }] });
    init(workersRef, { grid, tooltip: { trigger: 'axis', axisPointer: { type: 'cross' } }, xAxis: { type: 'category', data: labels, axisLabel }, yAxis: { type: 'value', minInterval: 1, axisLabel }, series: [
      { name: 'Active', type: 'line', stack: 'total', data: statsData.map((s: any) => s.active_workers || 0), smooth: true, symbol: 'none', lineStyle: { width: 0 }, areaStyle: { color: '#3b82f6' } },
      { name: 'Idle', type: 'line', stack: 'total', data: statsData.map((s: any) => s.idle_workers || 0), smooth: true, symbol: 'none', lineStyle: { width: 0 }, areaStyle: { color: '#94a3b8' } },
    ]});
    init(utilizationRef, { grid, tooltip: { trigger: 'axis' }, xAxis: { type: 'category', data: labels, axisLabel }, yAxis: { type: 'value', max: 100, axisLabel: { ...axisLabel, formatter: (v: number) => v + '%' } }, series: [{ name: 'Utilization', type: 'line', data: statsData.map((s: any) => s.avg_worker_utilization || 0), smooth: true, symbol: 'none', lineStyle: { width: 2, color: '#10b981' }, areaStyle: { color: { type: 'linear', x: 0, y: 0, x2: 0, y2: 1, colorStops: [{ offset: 0, color: 'rgba(16,185,129,0.3)' }, { offset: 1, color: 'rgba(16,185,129,0.05)' }] } } }] });
    init(idleTimeRef, { grid, tooltip: { trigger: 'axis' }, xAxis: { type: 'category', data: labels, axisLabel }, yAxis: { type: 'value', axisLabel: { ...axisLabel, formatter: (v: number) => v.toFixed(0) + 's' } }, series: [{ name: 'Idle Time', type: 'line', data: statsData.map((s: any) => s.avg_idle_duration_sec || 0), smooth: true, symbol: 'none', lineStyle: { width: 0 }, areaStyle: { color: '#f59e0b' } }] });
  }, [statsData, granularity, formatLabel, grid, axisLabel, dataZoom]);

  const hasData = statsData && statsData.length > 0;

  return (
    <>
      {/* Realtime Stats */}
      {realtimeData && (
        <div className="stats-grid mb-4" style={{ gridTemplateColumns: 'repeat(4, 1fr)' }}>
          <div className="stat-card"><div className="stat-content"><div className="stat-label">Active Workers</div><div className="stat-value">{realtimeData.workers?.active || 0}</div><div className="stat-change">{realtimeData.workers?.idle || 0} idle</div></div></div>
          <div className="stat-card"><div className="stat-content"><div className="stat-label">Tasks/min</div><div className="stat-value">{realtimeData.tasks?.completed_last_minute || 0}</div><div className="stat-change">{realtimeData.tasks?.running || 0} running</div></div></div>
          <div className="stat-card"><div className="stat-content"><div className="stat-label">Avg Execution</div><div className="stat-value">{((realtimeData.performance?.avg_execution_ms || 0) / 1000).toFixed(1)}s</div></div></div>
          <div className="stat-card"><div className="stat-content"><div className="stat-label">Queue Wait</div><div className="stat-value">{Math.round(realtimeData.performance?.avg_queue_wait_ms || 0)}ms</div></div></div>
        </div>
      )}
      {/* Controls */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16, padding: '12px 16px', background: '#fff', borderRadius: 8, border: '1px solid #e5e7eb' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
          {/* Time Range Dropdown */}
          <div style={{ position: 'relative' }}>
            <button
              onClick={() => setShowRangeDropdown(!showRangeDropdown)}
              style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '8px 16px', border: '1px solid #d1d5db', borderRadius: 6, background: '#fff', cursor: 'pointer', fontSize: 14, color: '#374151' }}
            >
              <span>📅</span>
              <span>{rangeLabels[timeRange]}</span>
              <span style={{ marginLeft: 4 }}>▼</span>
            </button>
            {showRangeDropdown && (
              <div style={{ position: 'absolute', top: '100%', left: 0, marginTop: 4, background: '#fff', border: '1px solid #d1d5db', borderRadius: 8, boxShadow: '0 4px 12px rgba(0,0,0,0.15)', padding: 8, zIndex: 100, minWidth: 160 }}>
                {Object.entries(rangeLabels).map(([key, label]) => (
                  <div key={key} onClick={() => { setTimeRange(key); setShowRangeDropdown(false); }} style={{ padding: '10px 16px', cursor: 'pointer', borderRadius: 4, fontSize: 14, color: '#374151', background: timeRange === key ? '#f3f4f6' : 'transparent' }}
                    onMouseEnter={(e) => (e.currentTarget.style.background = '#f3f4f6')}
                    onMouseLeave={(e) => (e.currentTarget.style.background = timeRange === key ? '#f3f4f6' : 'transparent')}
                  >{label}</div>
                ))}
              </div>
            )}
          </div>
          {/* Granularity */}
          <span style={{ fontSize: 14, color: '#6b7280' }}>Granularity: <strong>{granularity === '1m' ? 'Minute' : granularity === '1h' ? 'Hourly' : 'Daily'}</strong></span>
        </div>
        {/* Live Data Toggle */}
        <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 14, color: '#374151', cursor: 'pointer' }}>
          <input type="checkbox" checked={liveMode} onChange={(e) => setLiveMode(e.target.checked)} style={{ width: 16, height: 16, cursor: 'pointer' }} />
          <span style={{ width: 8, height: 8, borderRadius: '50%', background: liveMode ? '#48bb78' : '#d1d5db' }} />
          <span>View live data</span>
        </label>
      </div>
      <div className="charts-grid">
        <ChartCard chartRef={requestsRef} title="Requests" total={`Submitted: ${totals.submitted}`} legend={[{ color: '#3b82f6', label: `Submitted: ${totals.submitted}` }, { color: '#48bb78', label: `Completed: ${totals.completed}` }, { color: '#f56565', label: `Failed: ${totals.failed}` }, { color: '#ecc94b', label: `Retried: ${totals.retried}` }]} hasData={hasData} />
        <ChartCard chartRef={executionRef} title="Execution Time" legend={[{ color: '#3b82f6', label: 'P50' }, { color: '#f56565', label: 'P95' }]} hasData={hasData} />
        <ChartCard chartRef={delayRef} title="Queue Wait Time" hasData={hasData} />
        <ChartCard chartRef={workersRef} title="Worker Count" total={`Avg: ${avgValues.activeWorkers}`} legend={[{ color: '#3b82f6', label: 'Active' }, { color: '#94a3b8', label: 'Idle' }]} hasData={hasData} />
        <ChartCard chartRef={utilizationRef} title="Worker Utilization" total={`Avg: ${avgValues.workerUtilization}%`} hasData={hasData} />
        <ChartCard chartRef={idleTimeRef} title="Worker Idle Time (Avg)" hasData={hasData} />
        <ChartCard chartRef={coldStartCountRef} title="Cold Start Count" total={`Total: ${totals.coldStarts}`} hasData={hasData} />
        <ChartCard chartRef={coldStartTimeRef} title="Cold Start Time" hasData={hasData} />
      </div>
    </>
  );
};

const WorkersTab = ({ workers, endpoint }: { workers: WorkerWithPodInfo[]; endpoint: string }) => {
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [selectedWorker, setSelectedWorker] = useState<WorkerWithPodInfo | null>(null);
  const [drawerTab, setDrawerTab] = useState('tasks');
  const [taskDetailVisible, setTaskDetailVisible] = useState(false);
  const [selectedTask, setSelectedTask] = useState<Task | null>(null);

  // Sort workers by id for stable order
  const sortedWorkers = [...workers].sort((a, b) => (a.id || '').localeCompare(b.id || ''));

  const { data: podDetail } = useQuery({
    queryKey: ['pod-detail', endpoint, selectedWorker?.pod_name],
    queryFn: async () => (await api.endpoints.describePod(endpoint, selectedWorker!.pod_name!)).data,
    enabled: !!selectedWorker?.pod_name && drawerOpen && (drawerTab === 'details' || drawerTab === 'yaml'),
  });

  const { data: podYaml } = useQuery({
    queryKey: ['pod-yaml', endpoint, selectedWorker?.pod_name],
    queryFn: async () => (await api.apps.getPodYaml(endpoint, selectedWorker!.pod_name!)).data,
    enabled: !!selectedWorker?.pod_name && drawerOpen && drawerTab === 'yaml',
  });

  const { data: logsData, refetch: refetchLogs } = useQuery({
    queryKey: ['worker-logs', endpoint, selectedWorker?.pod_name],
    queryFn: async () => (await api.endpoints.logs(endpoint, 200, selectedWorker!.pod_name)).data,
    enabled: !!selectedWorker?.pod_name && drawerOpen && drawerTab === 'logs',
    refetchInterval: drawerTab === 'logs' ? 3000 : false,
  });

  const { data: tasksData } = useQuery({
    queryKey: ['worker-tasks', endpoint, selectedWorker?.id],
    queryFn: async () => (await api.tasks.list({ endpoint, worker_id: selectedWorker?.id, limit: 100 })).data,
    enabled: !!selectedWorker && drawerOpen && drawerTab === 'tasks',
  });

  // Use tasks from API response directly (already filtered by worker_id)
  const workerTasks = tasksData?.tasks || [];
  const active = sortedWorkers.filter(w => w.current_jobs > 0).length;

  const formatTime = (t: string) => { if (!t) return '-'; const d = new Date(t); const s = Math.floor((Date.now() - d.getTime()) / 1000); return s < 60 ? `${s}s ago` : s < 3600 ? `${Math.floor(s/60)}m ago` : d.toLocaleString(); };
  const getIdleTag = (w: WorkerWithPodInfo) => {
    if (w.current_jobs > 0) return <span className="tag running">Active</span>;
    if (!w.last_task_time) return <span className="tag" style={{ background: 'rgba(107,114,128,0.1)', color: '#6b7280' }}>New</span>;
    const m = Math.floor((Date.now() - new Date(w.last_task_time).getTime()) / 60000);
    if (m < 5) return <span className="tag success">{m}m idle</span>;
    if (m < 30) return <span className="tag pending">{m}m idle</span>;
    return <span className="tag failed">{m}m idle</span>;
  };

  return (
    <>
      <div className="stats-grid mb-4" style={{ gridTemplateColumns: 'repeat(4, 1fr)' }}>
        <div className="stat-card"><div className="stat-content"><div className="stat-label">Active</div><div className="stat-value" style={{ color: '#48bb78' }}>{active}</div></div></div>
        <div className="stat-card"><div className="stat-content"><div className="stat-label">Idle</div><div className="stat-value" style={{ color: '#f59e0b' }}>{sortedWorkers.length - active}</div></div></div>
        <div className="stat-card"><div className="stat-content"><div className="stat-label">Total</div><div className="stat-value">{sortedWorkers.length}</div></div></div>
        <div className="stat-card"><div className="stat-content"><div className="stat-label">Jobs</div><div className="stat-value">{sortedWorkers.reduce((s, w) => s + (w.current_jobs || 0), 0)}</div></div></div>
      </div>

      {/* Worker Cards */}
      {sortedWorkers.map(w => (
        <div key={w.id} className="worker-card" onClick={() => { setSelectedWorker(w); setDrawerTab('tasks'); setDrawerOpen(true); }} style={{ cursor: 'pointer' }}>
          <div className={`worker-icon ${w.current_jobs > 0 ? '' : 'idle'}`}>🖥️</div>
          <div className="worker-info" style={{ flex: 1 }}>
            <div className="worker-name">{w.pod_name || w.id}</div>
            <div className="worker-meta">
              <span className={`tag ${w.status?.toUpperCase() === 'ONLINE' ? 'running' : w.status?.toUpperCase() === 'BUSY' ? 'running' : w.status?.toUpperCase() === 'STARTING' ? 'pending' : w.status?.toUpperCase() === 'DRAINING' ? 'pending' : 'failed'}`} style={{ marginRight: 4 }}>{w.status}</span>
              {w.podStatus && <span className={`tag ${w.podStatus === 'Running' ? 'running' : w.podStatus === 'ImagePullBackOff' || w.podStatus === 'ErrImagePull' || w.podStatus === 'CrashLoopBackOff' ? 'failed' : 'pending'}`} style={{ marginRight: 8 }}>Pod: {w.podStatus}</span>}
              Jobs: {w.current_jobs || 0}/{w.concurrency || 1} • Heartbeat: {formatTime(w.last_heartbeat)} {w.version && `• v${w.version}`}
            </div>
            {/* Failure Information */}
            {w.failureType && (
              <div style={{ marginTop: 4, fontSize: 12 }}>
                <Tooltip title={
                  <div>
                    <div><strong>Error Type:</strong> {w.failureType}</div>
                    {w.failureReason && <div><strong>Reason:</strong> {w.failureReason}</div>}
                    {w.failureSuggestion && <div style={{ color: '#52c41a' }}><strong>Suggestion:</strong> {w.failureSuggestion}</div>}
                  </div>
                }>
                  <span style={{ color: '#f56565', cursor: 'help' }}>
                    ⚠️ {w.failureReason || w.failureType}
                  </span>
                </Tooltip>
              </div>
            )}
          </div>
          {getIdleTag(w)}
        </div>
      ))}
      {sortedWorkers.length === 0 && <div className="card" style={{ padding: 40, textAlign: 'center', color: '#6b7280' }}>No workers</div>}

      {/* Worker Drawer */}
      <Drawer title={`Worker: ${selectedWorker?.pod_name || selectedWorker?.id?.substring(0, 24) || '-'}`} open={drawerOpen} width="75%" onClose={() => setDrawerOpen(false)} destroyOnClose={drawerTab !== 'exec'}>
        <Tabs activeKey={drawerTab} onChange={setDrawerTab} items={[
          { key: 'tasks', label: '📋 Tasks', children: (
            <div className="table-container">
              <table><thead><tr><th>Task ID</th><th>Status</th><th>Created</th><th>Exec Time</th><th>Delay</th><th>Actions</th></tr></thead>
                <tbody>
                  {workerTasks.map((t: Task) => (
                    <tr key={t.id}>
                      <td style={{ fontFamily: 'monospace', fontSize: 11 }}>{t.id.substring(0, 20)}...</td>
                      <td><span className={`tag ${t.status === 'COMPLETED' ? 'success' : t.status === 'FAILED' ? 'failed' : t.status === 'IN_PROGRESS' ? 'running' : 'pending'}`}>{t.status}</span></td>
                      <td style={{ fontSize: 12 }}>{t.createdAt ? new Date(t.createdAt).toLocaleString() : '-'}</td>
                      <td style={{ fontSize: 12 }}>{t.executionTime ? `${(t.executionTime/1000).toFixed(2)}s` : '-'}</td>
                      <td style={{ fontSize: 12 }}>{t.delayTime ? `${(t.delayTime/1000).toFixed(2)}s` : '-'}</td>
                      <td><button className="btn btn-sm btn-outline" onClick={(e) => { e.stopPropagation(); setSelectedTask(t); setTaskDetailVisible(true); }}><EyeOutlined /> View</button></td>
                    </tr>
                  ))}
                  {workerTasks.length === 0 && <tr><td colSpan={6} style={{ textAlign: 'center', color: '#6b7280', padding: 20 }}>No tasks</td></tr>}
                </tbody>
              </table>
            </div>
          )},
          { key: 'logs', label: '📄 Logs', children: (
            <div>
              <button className="btn btn-outline mb-3" onClick={() => refetchLogs()}><SyncOutlined /> Refresh</button>
              <pre style={{ background: '#1f2937', color: '#e5e7eb', padding: 16, borderRadius: 8, fontSize: 12, maxHeight: 500, overflow: 'auto' }}>{logsData || 'No logs'}</pre>
            </div>
          )},
          { key: 'exec', label: '💻 Exec', children: selectedWorker ? (
            <div style={{ height: 500 }}>
              <TerminalComponent endpoint={endpoint} workerId={selectedWorker.pod_name || selectedWorker.id} />
            </div>
          ) : <div>No worker selected</div> },
          { key: 'details', label: '📊 Details', children: podDetail ? (
            <div>
              {/* Pod Info */}
              <div className="card mb-4">
                <div className="card-header"><h3>Pod Information</h3></div>
                <table className="info-table"><tbody>
                  <tr><td className="info-label">Name</td><td colSpan={3}>{podDetail.name}</td></tr>
                  <tr><td className="info-label">Namespace</td><td>{podDetail.namespace}</td><td className="info-label">UID</td><td style={{ fontSize: 11 }}>{podDetail.uid}</td></tr>
                  <tr><td className="info-label">Phase</td><td><span className={`tag ${podDetail.phase === 'Running' ? 'running' : 'pending'}`}>{podDetail.phase}</span></td><td className="info-label">Status</td><td>{podDetail.status} {podDetail.reason && `(${podDetail.reason})`}</td></tr>
                  <tr><td className="info-label">Pod IP</td><td>{podDetail.ip || '-'}</td><td className="info-label">Node</td><td>{podDetail.nodeName || '-'}</td></tr>
                  <tr><td className="info-label">Created</td><td>{new Date(podDetail.createdAt).toLocaleString()}</td><td className="info-label">Started</td><td>{podDetail.startedAt ? new Date(podDetail.startedAt).toLocaleString() : '-'}</td></tr>
                  <tr><td className="info-label">Restarts</td><td colSpan={3}>{podDetail.restartCount}</td></tr>
                </tbody></table>
              </div>
              {/* Containers */}
              {podDetail.containers?.length > 0 && (
                <div className="card mb-4">
                  <div className="card-header"><h3>Containers ({podDetail.containers.length})</h3></div>
                  <div className="table-container">
                    <table><thead><tr><th>Name</th><th>Image</th><th>State</th><th>Ready</th><th>Restarts</th></tr></thead>
                      <tbody>{podDetail.containers.map((c: any) => (
                        <tr key={c.name}>
                          <td style={{ fontWeight: 500 }}>{c.name}</td>
                          <td style={{ fontSize: 11, maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis' }}>{c.image}</td>
                          <td><span className={`tag ${c.state === 'Running' ? 'running' : 'pending'}`}>{c.state}</span>{c.reason && <span style={{ fontSize: 11, color: '#6b7280' }}> ({c.reason})</span>}</td>
                          <td>{c.ready ? '✅' : '❌'}</td>
                          <td>{c.restartCount}</td>
                        </tr>
                      ))}</tbody>
                    </table>
                  </div>
                </div>
              )}
              {/* Conditions */}
              {podDetail.conditions?.length > 0 && (
                <div className="card mb-4">
                  <div className="card-header"><h3>Conditions</h3></div>
                  <div className="table-container">
                    <table><thead><tr><th>Type</th><th>Status</th><th>Reason</th><th>Message</th><th>Last Transition</th></tr></thead>
                      <tbody>{podDetail.conditions.map((c: any) => (
                        <tr key={c.type}>
                          <td>{c.type}</td>
                          <td><span className={`tag ${c.status === 'True' ? 'success' : 'failed'}`}>{c.status}</span></td>
                          <td style={{ fontSize: 12 }}>{c.reason || '-'}</td>
                          <td style={{ fontSize: 12, maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis' }}>{c.message || '-'}</td>
                          <td style={{ fontSize: 12 }}>{c.lastTransitionTime ? new Date(c.lastTransitionTime).toLocaleString() : '-'}</td>
                        </tr>
                      ))}</tbody>
                    </table>
                  </div>
                </div>
              )}
              {/* Events */}
              {podDetail.events?.length > 0 && (
                <div className="card mb-4">
                  <div className="card-header"><h3>Events ({podDetail.events.length})</h3></div>
                  <div style={{ padding: 16, maxHeight: 300, overflow: 'auto' }}>
                    {podDetail.events.map((e: any, i: number) => (
                      <div key={i} style={{ marginBottom: 12, paddingBottom: 12, borderBottom: '1px solid #f3f4f6' }}>
                        <div className="flex gap-2 items-center">
                          <span className={`tag ${e.type === 'Warning' ? 'failed' : 'running'}`}>{e.type}</span>
                          <span style={{ fontWeight: 500 }}>{e.reason}</span>
                          {e.count > 1 && <span style={{ fontSize: 11, color: '#6b7280' }}>(x{e.count})</span>}
                        </div>
                        <div style={{ fontSize: 13, marginTop: 4 }}>{e.message}</div>
                        <div style={{ fontSize: 11, color: '#9ca3af', marginTop: 2 }}>First: {new Date(e.firstSeen).toLocaleString()} • Last: {new Date(e.lastSeen).toLocaleString()}</div>
                      </div>
                    ))}
                  </div>
                </div>
              )}
              {/* Tolerations */}
              {podDetail.tolerations && podDetail.tolerations.length > 0 && (
                <div className="card mb-4">
                  <div className="card-header"><h3>Tolerations ({podDetail.tolerations.length})</h3></div>
                  <div className="table-container">
                    <table><thead><tr><th>Key</th><th>Operator</th><th>Value</th><th>Effect</th></tr></thead>
                      <tbody>{podDetail.tolerations.map((t: any, i: number) => (
                        <tr key={i}><td><code>{t.key}</code></td><td>{t.operator}</td><td>{t.value || '-'}</td><td>{t.effect}</td></tr>
                      ))}</tbody>
                    </table>
                  </div>
                </div>
              )}
              {/* Volumes */}
              {podDetail.volumes && podDetail.volumes.length > 0 && (
                <div className="card">
                  <div className="card-header"><h3>Volumes ({podDetail.volumes.length})</h3></div>
                  <div className="table-container">
                    <table><thead><tr><th>Name</th><th>Type</th><th>Source</th></tr></thead>
                      <tbody>{podDetail.volumes.map((v: any) => (
                        <tr key={v.name}><td>{v.name}</td><td><span className="tag">{v.type}</span></td><td style={{ fontSize: 11 }}>{v.source ? JSON.stringify(v.source) : '-'}</td></tr>
                      ))}</tbody>
                    </table>
                  </div>
                </div>
              )}
            </div>
          ) : <div style={{ padding: 20, color: '#6b7280' }}>Loading...</div> },
          { key: 'yaml', label: '📝 YAML', children: (
            <div>
              <button className="btn btn-outline mb-3" onClick={() => { if (podYaml) { navigator.clipboard.writeText(podYaml); message.success('Copied!'); } }}><CopyOutlined /> Copy YAML</button>
              <pre style={{ background: '#1f2937', color: '#e5e7eb', padding: 16, borderRadius: 8, fontSize: 11, maxHeight: 600, overflow: 'auto', whiteSpace: 'pre-wrap' }}>{podYaml || 'Loading...'}</pre>
            </div>
          )},
        ]} />
      </Drawer>

      {/* Task Detail Modal */}
      <Modal title="Task Details" open={taskDetailVisible} onCancel={() => setTaskDetailVisible(false)} footer={null} width={900}>
        {selectedTask && <WorkerTaskDetail task={selectedTask} />}
      </Modal>
    </>
  );
};

// Task Detail for Worker Tab
const WorkerTaskDetail = ({ task }: { task: Task }) => {
  const { data: fullTask } = useQuery({
    queryKey: ['task-detail', task.id],
    queryFn: async () => (await api.tasks.get(task.id)).data,
  });
  const t = fullTask || task;
  const getStatusClass = (s: string) => s === 'COMPLETED' ? 'success' : s === 'FAILED' ? 'failed' : s === 'IN_PROGRESS' ? 'running' : 'pending';

  return (
    <div>
      <div className="form-row">
        <div className="form-group"><label className="form-label">Task ID</label><div style={{ fontFamily: 'monospace', fontSize: 12, wordBreak: 'break-all' }}>{t.id}</div></div>
        <div className="form-group"><label className="form-label">Status</label><span className={`tag ${getStatusClass(t.status)}`}>{t.status}</span></div>
      </div>
      <div className="form-row">
        <div className="form-group"><label className="form-label">Endpoint</label><div>{t.endpoint}</div></div>
        <div className="form-group"><label className="form-label">Worker</label><div style={{ fontFamily: 'monospace', fontSize: 12, wordBreak: 'break-all' }}>{t.workerId || '-'}</div></div>
      </div>
      <div className="form-row">
        <div className="form-group"><label className="form-label">Created</label><div>{t.createdAt ? new Date(t.createdAt).toLocaleString() : '-'}</div></div>
        <div className="form-group"><label className="form-label">Execution Time</label><div>{t.executionTime ? `${(t.executionTime/1000).toFixed(2)}s` : '-'}</div></div>
      </div>
      {t.input && <div className="form-group"><label className="form-label">Input</label><pre style={{ background: '#f3f4f6', padding: 12, borderRadius: 6, fontSize: 12, maxHeight: 200, overflow: 'auto' }}>{JSON.stringify(t.input, null, 2)}</pre></div>}
      {t.output && <div className="form-group"><label className="form-label">Output</label><pre style={{ background: '#f3f4f6', padding: 12, borderRadius: 6, fontSize: 12, maxHeight: 200, overflow: 'auto' }}>{JSON.stringify(t.output, null, 2)}</pre></div>}
      {t.error && <div className="form-group"><label className="form-label">Error</label><pre style={{ background: '#fef2f2', color: '#dc2626', padding: 12, borderRadius: 6, fontSize: 12 }}>{t.error}</pre></div>}
    </div>
  );
};

// Lazy load Terminal to avoid SSR issues
const TerminalComponent = ({ endpoint, workerId }: { endpoint: string; workerId: string }) => {
  const [Terminal, setTerminal] = useState<any>(null);
  useEffect(() => { import('@/components/Terminal').then(m => setTerminal(() => m.default)); }, []);
  if (!Terminal) return <div style={{ padding: 20 }}>Loading terminal...</div>;
  return <Terminal endpoint={endpoint} workerId={workerId} />;
};

// Tasks Tab with search, pagination, detail view
const TasksTab = ({ endpoint, workers }: { endpoint: string; workers: WorkerWithPodInfo[] }) => {
  const [searchInput, setSearchInput] = useState('');
  const [statusInput, setStatusInput] = useState('all');
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState('all');
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(20);
  const [selectedTask, setSelectedTask] = useState<Task | null>(null);
  const [detailOpen, setDetailOpen] = useState(false);

  const { data: tasksData, isLoading, refetch } = useQuery({
    queryKey: ['endpoint-tasks', endpoint, statusFilter, search, page, pageSize],
    queryFn: async () => {
      const params: any = { endpoint, limit: pageSize, offset: page * pageSize };
      if (statusFilter !== 'all') params.status = statusFilter;
      if (search) params.task_id = search;
      return (await api.tasks.list(params)).data;
    },
  });

  const handleSearch = () => {
    setSearch(searchInput);
    setStatusFilter(statusInput);
    setPage(0);
  };

  const cancelMutation = useMutation({
    mutationFn: (taskId: string) => api.tasks.cancel(taskId),
    onSuccess: () => { message.success('Task cancelled'); refetch(); },
    onError: (e: any) => message.error(e.response?.data?.error || 'Failed'),
  });

  const tasks = tasksData?.tasks || [];
  const total = tasksData?.total || 0;
  const workerMap = new Map(workers.map(w => [w.id, w]));

  const getStatusClass = (s: string) => s === 'COMPLETED' ? 'success' : s === 'FAILED' ? 'failed' : s === 'IN_PROGRESS' ? 'running' : 'pending';

  return (
    <>
      {/* Filters */}
      <div className="flex justify-between items-center mb-4">
        <div className="filters">
          <Input placeholder="Search task ID..." prefix={<SearchOutlined />} value={searchInput} onChange={e => setSearchInput(e.target.value)} onPressEnter={handleSearch} style={{ width: 240 }} />
          <Select value={statusInput} onChange={setStatusInput} style={{ width: 140 }} options={[
            { value: 'all', label: 'All Status' },
            { value: 'PENDING', label: 'Pending' },
            { value: 'IN_PROGRESS', label: 'In Progress' },
            { value: 'COMPLETED', label: 'Completed' },
            { value: 'FAILED', label: 'Failed' },
          ]} />
          <button className="btn btn-blue" onClick={handleSearch}><SearchOutlined /> Search</button>
          <button className="btn btn-outline" onClick={() => { setSearchInput(''); setStatusInput('all'); setSearch(''); setStatusFilter('all'); setPage(0); }}>Reset</button>
        </div>
        <span style={{ color: '#6b7280', fontSize: 13 }}>Total: {total}</span>
      </div>

      {/* Table */}
      <div className="card">
        <div className="table-container">
          <table>
            <thead><tr><th>Task ID</th><th>Status</th><th>Worker</th><th>Created</th><th>Exec Time</th><th>Delay</th><th>Actions</th></tr></thead>
            <tbody>
              {isLoading ? <tr><td colSpan={7}><div className="loading"><div className="spinner"></div></div></td></tr> :
               tasks.length === 0 ? <tr><td colSpan={7} style={{ textAlign: 'center', color: '#6b7280', padding: 40 }}>No tasks</td></tr> :
               tasks.map((t: Task) => {
                 const worker = t.workerId ? workerMap.get(t.workerId) : null;
                 return (
                   <tr key={t.id}>
                     <td><Tooltip title={t.id}><span style={{ fontFamily: 'monospace', fontSize: 11 }}>{t.id.substring(0, 20)}...</span></Tooltip></td>
                     <td><span className={`tag ${getStatusClass(t.status)}`}>{t.status}</span></td>
                     <td>
                       {t.workerId ? (
                         worker ? (
                           <Tooltip title={`Click to view worker: ${t.workerId}`}>
                             <span style={{ fontSize: 12, color: '#1da1f2', cursor: 'pointer' }} onClick={() => { /* TODO: jump to worker */ }}>
                               {t.workerId.substring(0, 12)}...
                             </span>
                           </Tooltip>
                         ) : (
                           <Tooltip title={t.workerId}><span style={{ fontSize: 12, color: '#6b7280' }}>{t.workerId.substring(0, 12)}...</span></Tooltip>
                         )
                       ) : '-'}
                     </td>
                     <td style={{ fontSize: 12 }}>{t.createdAt ? new Date(t.createdAt).toLocaleString() : '-'}</td>
                     <td style={{ fontSize: 12 }}>{t.executionTime ? `${(t.executionTime/1000).toFixed(2)}s` : '-'}</td>
                     <td style={{ fontSize: 12 }}>{t.delayTime ? `${(t.delayTime/1000).toFixed(2)}s` : '-'}</td>
                     <td>
                       <div className="flex gap-2">
                         <button className="btn btn-sm btn-outline" onClick={() => { setSelectedTask(t); setDetailOpen(true); }}><EyeOutlined /></button>
                         {(t.status === 'PENDING' || t.status === 'IN_PROGRESS') && (
                           <Popconfirm title="Cancel?" onConfirm={() => cancelMutation.mutate(t.id)}>
                             <button className="btn btn-sm btn-outline" style={{ color: '#f56565' }}><StopOutlined /></button>
                           </Popconfirm>
                         )}
                       </div>
                     </td>
                   </tr>
                 );
               })}
            </tbody>
          </table>
        </div>
        {/* Pagination */}
        <div className="flex justify-between items-center" style={{ padding: '12px 16px', borderTop: '1px solid #f3f4f6' }}>
          <div className="flex gap-2 items-center">
            <span style={{ fontSize: 13, color: '#6b7280' }}>Show</span>
            <Select value={pageSize} onChange={v => { setPageSize(v); setPage(0); }} size="small" style={{ width: 70 }} options={[
              { value: 10, label: '10' },
              { value: 20, label: '20' },
              { value: 50, label: '50' },
              { value: 100, label: '100' },
            ]} />
            <span style={{ fontSize: 13, color: '#6b7280' }}>/ page • Total: {total}</span>
          </div>
          <div className="flex gap-2 items-center">
            <span style={{ fontSize: 13, color: '#6b7280' }}>Page {page + 1} of {Math.ceil(total / pageSize) || 1}</span>
            <button className="btn btn-sm btn-outline" disabled={page === 0} onClick={() => setPage(p => p - 1)}>Prev</button>
            <button className="btn btn-sm btn-outline" disabled={(page + 1) * pageSize >= total} onClick={() => setPage(p => p + 1)}>Next</button>
          </div>
        </div>
      </div>

      {/* Detail Modal */}
      <Modal title="Task Details" open={detailOpen} onCancel={() => setDetailOpen(false)} footer={null} width={900}>
        {selectedTask && <TaskDetailContent task={selectedTask} getStatusClass={getStatusClass} />}
      </Modal>
    </>
  );
};

// Task Detail Content with Timeline and Execution History
const TaskDetailContent = ({ task, getStatusClass }: { task: Task; getStatusClass: (s: string) => string }) => {
  // Fetch full task detail from /v1/status/{taskId}
  const { data: fullTask } = useQuery({
    queryKey: ['task-detail', task.id],
    queryFn: async () => (await api.tasks.get(task.id)).data,
  });
  const { data: timeline } = useQuery({
    queryKey: ['task-timeline', task.id],
    queryFn: async () => (await api.tasks.getTimeline(task.id)).data,
  });
  const { data: execHistory } = useQuery({
    queryKey: ['task-exec-history', task.id],
    queryFn: async () => (await api.tasks.getExecutionHistory(task.id)).data,
  });

  // Use fullTask if available, fallback to task from list
  const t = fullTask || task;

  return (
    <div>
      {/* Basic Info */}
      <div className="form-row">
        <div className="form-group"><label className="form-label">Task ID</label><div style={{ fontFamily: 'monospace', fontSize: 12, wordBreak: 'break-all' }}>{t.id}</div></div>
        <div className="form-group"><label className="form-label">Status</label><span className={`tag ${getStatusClass(t.status)}`}>{t.status}</span></div>
      </div>
      <div className="form-row">
        <div className="form-group"><label className="form-label">Endpoint</label><div>{t.endpoint}</div></div>
        <div className="form-group"><label className="form-label">Worker</label><div style={{ fontFamily: 'monospace', fontSize: 12, wordBreak: 'break-all' }}>{t.workerId || '-'}</div></div>
      </div>
      <div className="form-row">
        <div className="form-group"><label className="form-label">Created</label><div>{t.createdAt ? new Date(t.createdAt).toLocaleString() : '-'}</div></div>
        <div className="form-group"><label className="form-label">Execution Time</label><div>{t.executionTime ? `${(t.executionTime/1000).toFixed(2)}s` : '-'}</div></div>
      </div>
      <div className="form-row">
        <div className="form-group"><label className="form-label">Delay Time</label><div>{t.delayTime ? `${(t.delayTime/1000).toFixed(2)}s` : '-'}</div></div>
      </div>

      {/* Input - always show */}
      <div style={{ marginTop: 16 }}>
        <label className="form-label">Input</label>
        <pre style={{ background: '#f9fafb', padding: 12, borderRadius: 6, fontSize: 12, overflow: 'auto', maxHeight: 200, whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>{t.input ? JSON.stringify(t.input, null, 2) : '-'}</pre>
      </div>

      {/* Output */}
      {t.output && (
        <div style={{ marginTop: 16 }}>
          <label className="form-label">Output</label>
          <pre style={{ background: '#f9fafb', padding: 12, borderRadius: 6, fontSize: 12, overflow: 'auto', maxHeight: 300, whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>{JSON.stringify(t.output, null, 2)}</pre>
        </div>
      )}

      {/* Error */}
      {t.error && (
        <div style={{ marginTop: 16 }}>
          <label className="form-label">Error</label>
          <pre style={{ background: '#fef2f2', padding: 12, borderRadius: 6, fontSize: 12, color: '#f56565', whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>{t.error}</pre>
        </div>
      )}

      {/* Timeline */}
      {timeline?.timeline && timeline.timeline.length > 0 && (
        <div style={{ marginTop: 20 }}>
          <label className="form-label">Timeline</label>
          <Timeline style={{ marginTop: 12 }} items={timeline.timeline.map((e: any) => ({
            color: e.to_status === 'COMPLETED' ? 'green' : e.to_status === 'FAILED' ? 'red' : e.to_status === 'IN_PROGRESS' ? 'blue' : 'gray',
            children: (
              <div>
                <div><strong>{e.event_type}</strong>{e.from_status && e.to_status && <span style={{ color: '#6b7280' }}> ({e.from_status} → {e.to_status})</span>}</div>
                <div style={{ fontSize: 12, color: '#6b7280' }}>{new Date(e.event_time).toLocaleString()}</div>
                {e.worker_id && <div style={{ fontSize: 11 }}><code>Worker: {e.worker_id.substring(0, 20)}...</code></div>}
                {e.worker_pod_name && <div style={{ fontSize: 11 }}><code>Pod: {e.worker_pod_name}</code></div>}
                {e.error_message && <div style={{ fontSize: 12, color: '#f56565' }}>{e.error_message}</div>}
              </div>
            ),
          }))} />
        </div>
      )}

      {/* Execution History */}
      {execHistory?.history && execHistory.history.length > 0 && (
        <div style={{ marginTop: 16 }}>
          <label className="form-label">Execution History</label>
          <div style={{ background: '#f9fafb', padding: 12, borderRadius: 6, marginTop: 8 }}>
            <table style={{ width: '100%', fontSize: 12 }}>
              <thead><tr style={{ color: '#6b7280' }}><th style={{ textAlign: 'left', padding: 4 }}>Worker</th><th style={{ textAlign: 'left', padding: 4 }}>Start</th><th style={{ textAlign: 'left', padding: 4 }}>End</th><th style={{ textAlign: 'left', padding: 4 }}>Duration</th></tr></thead>
              <tbody>
                {execHistory.history.map((h: any, i: number) => (
                  <tr key={i}>
                    <td style={{ padding: 4, fontFamily: 'monospace' }}>{h.worker_id?.substring(0, 16) || '-'}</td>
                    <td style={{ padding: 4 }}>{h.start_time ? new Date(h.start_time).toLocaleString() : '-'}</td>
                    <td style={{ padding: 4 }}>{h.end_time ? new Date(h.end_time).toLocaleString() : '-'}</td>
                    <td style={{ padding: 4 }}>{h.duration_seconds ? `${h.duration_seconds.toFixed(2)}s` : '-'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  );
};

const SettingsTab = ({ endpoint, name }: { endpoint: AppInfo; name: string }) => {
  const queryClient = useQueryClient();

  const { data: specs } = useQuery({ queryKey: ['specs'], queryFn: async () => (await api.specs.list()).data });
  const { data: pvcs } = useQuery({ queryKey: ['pvcs'], queryFn: async () => (await api.k8s.listPVCs()).data });

  const updateMetadata = useMutation({
    mutationFn: (data: UpdateEndpointConfigRequest) => api.endpoints.updateMetadata(name, data),
    onSuccess: () => { message.success('Saved'); queryClient.invalidateQueries({ queryKey: ['endpoint', name] }); },
    onError: (e: any) => message.error(e.response?.data?.error || 'Failed'),
  });

  const updateDeployment = useMutation({
    mutationFn: (data: UpdateDeploymentRequest) => api.endpoints.updateDeployment(name, data),
    onSuccess: () => { message.success('Saved'); queryClient.invalidateQueries({ queryKey: ['endpoint', name] }); },
    onError: (e: any) => message.error(e.response?.data?.error || 'Failed'),
  });

  return (
    <Collapse defaultActiveKey={['basic']} accordion items={[
      { key: 'basic', label: '📝 Basic Information', children: <BasicInfoPanel endpoint={endpoint} onSave={d => updateMetadata.mutate(d)} saving={updateMetadata.isPending} /> },
      { key: 'deployment', label: '🚀 Deployment Configuration', children: <DeploymentPanel endpoint={endpoint} name={name} specs={specs || []} onSave={d => updateDeployment.mutate(d)} saving={updateDeployment.isPending} /> },
      { key: 'autoscaler', label: '⚡ AutoScaler Configuration', children: <AutoScalerPanel endpoint={endpoint} onSave={d => updateMetadata.mutate(d)} saving={updateMetadata.isPending} /> },
      { key: 'env', label: '🔧 Environment Variables', children: <EnvVarsPanel endpoint={endpoint} name={name} onSave={d => updateDeployment.mutate(d)} saving={updateDeployment.isPending} /> },
      { key: 'volumes', label: '💾 Volume Mounts', children: <VolumeMountsPanel endpoint={endpoint} name={name} pvcs={pvcs || []} onSave={d => updateDeployment.mutate(d)} saving={updateDeployment.isPending} /> },
    ]} />
  );
};

const BasicInfoPanel = ({ endpoint, onSave, saving }: { endpoint: AppInfo; onSave: (d: UpdateEndpointConfigRequest) => void; saving: boolean }) => {
  const [form] = Form.useForm();
  useEffect(() => { form.setFieldsValue({ displayName: endpoint.displayName || '', description: endpoint.description || '', taskTimeout: endpoint.taskTimeout || 3600, maxPendingTasks: endpoint.maxPendingTasks || 100, imagePrefix: endpoint.imagePrefix || '' }); }, [endpoint, form]);
  return (
    <Form form={form} layout="vertical" onFinish={onSave}>
      <div className="form-row"><Form.Item name="displayName" label="Display Name"><Input /></Form.Item><Form.Item name="imagePrefix" label="Image Prefix"><Input placeholder="e.g., wavespeed/model:prefix-" /></Form.Item></div>
      <Form.Item name="description" label="Description"><Input.TextArea rows={2} /></Form.Item>
      <div className="form-row"><Form.Item name="taskTimeout" label="Task Timeout (s)"><InputNumber min={1} style={{ width: '100%' }} /></Form.Item><Form.Item name="maxPendingTasks" label="Max Pending Tasks"><InputNumber min={1} style={{ width: '100%' }} /></Form.Item></div>
      <button type="submit" className="btn btn-blue" disabled={saving}>{saving ? 'Saving...' : 'Save'}</button>
    </Form>
  );
};

const DeploymentPanel = ({ endpoint, name, specs, onSave, saving }: { endpoint: AppInfo; name: string; specs: SpecInfo[]; onSave: (d: UpdateDeploymentRequest) => void; saving: boolean }) => {
  const [form] = Form.useForm();
  useEffect(() => { form.setFieldsValue({ specName: endpoint.specName || '', image: endpoint.image || '', replicas: endpoint.replicas || 1, shmSize: endpoint.shmSize || '', enablePtrace: endpoint.enablePtrace || false }); }, [endpoint, form]);
  return (
    <Form form={form} layout="vertical" onFinish={v => onSave({ endpoint: name, ...v })}>
      <Form.Item name="specName" label="GPU Spec"><Select options={specs.map(s => ({ label: `${s.displayName} (${s.name})`, value: s.name }))} /></Form.Item>
      <Form.Item name="image" label="Docker Image"><Input /></Form.Item>
      <div className="form-row"><Form.Item name="replicas" label="Replicas"><InputNumber min={0} style={{ width: '100%' }} /></Form.Item><Form.Item name="shmSize" label="Shared Memory"><Input placeholder="e.g., 16Gi" /></Form.Item></div>
      <Form.Item name="enablePtrace" label="Enable Debugging (ptrace)" valuePropName="checked"><Switch /></Form.Item>
      <button type="submit" className="btn btn-blue" disabled={saving}>{saving ? 'Saving...' : 'Save'}</button>
    </Form>
  );
};

const AutoScalerPanel = ({ endpoint, onSave, saving }: { endpoint: AppInfo; onSave: (d: UpdateEndpointConfigRequest) => void; saving: boolean }) => {
  const [form] = Form.useForm();
  useEffect(() => { form.setFieldsValue({ autoscalerEnabled: endpoint.autoscalerEnabled || '', minReplicas: endpoint.minReplicas || 0, maxReplicas: endpoint.maxReplicas || 10, scaleUpThreshold: endpoint.scaleUpThreshold || 1, scaleDownIdleTime: endpoint.scaleDownIdleTime || 300, scaleUpCooldown: endpoint.scaleUpCooldown || 60, scaleDownCooldown: endpoint.scaleDownCooldown || 120, priority: endpoint.priority || 50, enableDynamicPrio: endpoint.enableDynamicPrio || false, highLoadThreshold: endpoint.highLoadThreshold || 10, priorityBoost: endpoint.priorityBoost || 20 }); }, [endpoint, form]);
  return (
    <Form form={form} layout="vertical" onFinish={onSave}>
      <Form.Item name="autoscalerEnabled" label="AutoScaler Override"><Select options={[{ value: '', label: 'Default' }, { value: 'enabled', label: 'Force On' }, { value: 'disabled', label: 'Force Off' }]} /></Form.Item>
      <div className="form-row"><Form.Item name="minReplicas" label="Min Replicas"><InputNumber min={0} style={{ width: '100%' }} /></Form.Item><Form.Item name="maxReplicas" label="Max Replicas"><InputNumber min={1} style={{ width: '100%' }} /></Form.Item></div>
      <div className="form-row"><Form.Item name="scaleUpThreshold" label="Scale Up Threshold"><InputNumber min={1} style={{ width: '100%' }} /></Form.Item><Form.Item name="scaleDownIdleTime" label="Scale Down Idle (s)"><InputNumber min={0} style={{ width: '100%' }} /></Form.Item></div>
      <div className="form-row"><Form.Item name="scaleUpCooldown" label="Scale Up Cooldown (s)"><InputNumber min={0} style={{ width: '100%' }} /></Form.Item><Form.Item name="scaleDownCooldown" label="Scale Down Cooldown (s)"><InputNumber min={0} style={{ width: '100%' }} /></Form.Item></div>
      <div className="form-row"><Form.Item name="priority" label="Priority (0-100)"><InputNumber min={0} max={100} style={{ width: '100%' }} /></Form.Item><Form.Item name="enableDynamicPrio" label="Dynamic Priority" valuePropName="checked"><Switch /></Form.Item></div>
      <div className="form-row"><Form.Item name="highLoadThreshold" label="High Load Threshold"><InputNumber min={1} style={{ width: '100%' }} /></Form.Item><Form.Item name="priorityBoost" label="Priority Boost"><InputNumber min={0} max={100} style={{ width: '100%' }} /></Form.Item></div>
      <button type="submit" className="btn btn-blue" disabled={saving}>{saving ? 'Saving...' : 'Save'}</button>
    </Form>
  );
};

const EnvVarsPanel = ({ endpoint, name, onSave, saving }: { endpoint: AppInfo; name: string; onSave: (d: UpdateDeploymentRequest) => void; saving: boolean }) => {
  const [envVars, setEnvVars] = useState<{ key: string; value: string }[]>([]);
  const [mode, setMode] = useState<'form' | 'text'>('form');
  const [envText, setEnvText] = useState('');
  useEffect(() => { const vars = endpoint.env ? Object.entries(endpoint.env).map(([key, value]) => ({ key, value })) : []; setEnvVars(vars.length > 0 ? vars : [{ key: '', value: '' }]); }, [endpoint]);
  const handleSave = () => {
    const env: Record<string, string> = {};
    if (mode === 'text') {
      envText.split('\n').filter(line => line.includes('=')).forEach(line => {
        const idx = line.indexOf('=');
        const key = line.slice(0, idx).trim();
        const value = line.slice(idx + 1).trim();
        if (key) env[key] = value;
      });
    } else {
      envVars.filter(v => v.key).forEach(v => { env[v.key] = v.value || ''; });
    }
    onSave({ endpoint: name, env });
  };
  const toggleMode = () => {
    if (mode === 'form') {
      setEnvText(envVars.filter(v => v.key).map(v => `${v.key}=${v.value || ''}`).join('\n'));
    } else {
      const vars = envText.split('\n').filter(line => line.includes('=')).map(line => {
        const idx = line.indexOf('=');
        return { key: line.slice(0, idx).trim(), value: line.slice(idx + 1).trim() };
      });
      setEnvVars(vars.length ? vars : [{ key: '', value: '' }]);
    }
    setMode(mode === 'form' ? 'text' : 'form');
  };
  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 8 }}>
        <Button type="link" size="small" onClick={toggleMode}>{mode === 'form' ? 'Switch to Text' : 'Switch to Form'}</Button>
      </div>
      {mode === 'text' ? (
        <Input.TextArea value={envText} onChange={e => setEnvText(e.target.value)} placeholder="KEY=value&#10;ANOTHER_KEY=another value" rows={8} style={{ fontFamily: 'monospace', marginBottom: 12 }} />
      ) : (
        <>
          {envVars.map((v, i) => (
            <div key={i} className="flex gap-2 mb-2">
              <Input value={v.key} onChange={e => { const n = [...envVars]; n[i].key = e.target.value; setEnvVars(n); }} placeholder="KEY" style={{ flex: 1 }} />
              <Input value={v.value} onChange={e => { const n = [...envVars]; n[i].value = e.target.value; setEnvVars(n); }} placeholder="value" style={{ flex: 2 }} />
              <button type="button" className="btn btn-outline btn-icon" onClick={() => setEnvVars(envVars.filter((_, idx) => idx !== i))}><DeleteOutlined /></button>
            </div>
          ))}
          <button type="button" className="btn btn-outline mb-3" onClick={() => setEnvVars([...envVars, { key: '', value: '' }])}><PlusOutlined /> Add</button>
        </>
      )}
      <div><button type="button" className="btn btn-blue" onClick={handleSave} disabled={saving}>{saving ? 'Saving...' : 'Save'}</button></div>
    </div>
  );
};

const VolumeMountsPanel = ({ endpoint, name, pvcs, onSave, saving }: { endpoint: AppInfo; name: string; pvcs: PVCInfo[]; onSave: (d: UpdateDeploymentRequest) => void; saving: boolean }) => {
  const [mounts, setMounts] = useState<{ pvcName: string; mountPath: string }[]>([]);
  useEffect(() => { const vms = endpoint.volumeMounts || []; setMounts(vms.length > 0 ? vms : [{ pvcName: '', mountPath: '' }]); }, [endpoint]);
  const handleSave = () => { onSave({ endpoint: name, volumeMounts: mounts.filter(m => m.pvcName && m.mountPath) }); };
  return (
    <div>
      {mounts.map((m, i) => (
        <div key={i} className="flex gap-2 mb-2">
          <Select value={m.pvcName} onChange={v => { const n = [...mounts]; n[i].pvcName = v; setMounts(n); }} placeholder="Select PVC" style={{ flex: 1 }} options={pvcs.map(p => ({ label: `${p.name} (${p.capacity})`, value: p.name }))} />
          <Input value={m.mountPath} onChange={e => { const n = [...mounts]; n[i].mountPath = e.target.value; setMounts(n); }} placeholder="/mount/path" style={{ flex: 1 }} />
          <button type="button" className="btn btn-outline btn-icon" onClick={() => setMounts(mounts.filter((_, idx) => idx !== i))}><DeleteOutlined /></button>
        </div>
      ))}
      <button type="button" className="btn btn-outline mb-3" onClick={() => setMounts([...mounts, { pvcName: '', mountPath: '' }])}><PlusOutlined /> Add</button>
      <div><button type="button" className="btn btn-blue" onClick={handleSave} disabled={saving}>{saving ? 'Saving...' : 'Save'}</button></div>
    </div>
  );
};

const ScalingHistoryTab = ({ endpoint }: { endpoint: string }) => {
  const { data: history, isLoading } = useQuery({
    queryKey: ['scaling-history', endpoint],
    queryFn: async () => (await api.autoscaler.getHistory(endpoint, 50)).data,
    refetchInterval: 30000,
  });

  if (isLoading) return <div className="loading"><div className="spinner"></div></div>;
  if (!history?.length) return <div className="empty-state"><p>No scaling events</p></div>;

  return (
    <div className="card">
      <div className="table-container">
        <table>
          <thead>
            <tr>
              <th>Time</th>
              <th>Action</th>
              <th>Replicas</th>
              <th>Reason</th>
              <th>Queue</th>
              <th>Priority</th>
            </tr>
          </thead>
          <tbody>
            {history.map((e: any) => (
              <tr key={e.id || e.timestamp}>
                <td style={{ fontSize: 12, color: '#6b7280' }}>{new Date(e.timestamp).toLocaleString()}</td>
                <td>
                  <span className={`tag ${e.action === 'scale_up' ? 'running' : 'stopped'}`}>
                    {e.action === 'scale_up' ? '↑ Scale Up' : '↓ Scale Down'}
                  </span>
                </td>
                <td>{e.fromReplicas} → {e.toReplicas}</td>
                <td style={{ maxWidth: 300, overflow: 'hidden', textOverflow: 'ellipsis' }}>{e.reason}</td>
                <td>{e.queueLength}</td>
                <td>{e.priority}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
};

export default EndpointDetailPage;
