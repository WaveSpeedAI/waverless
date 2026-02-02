import { useState, useEffect } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { Modal, Form, Input, Select, InputNumber, message, Collapse, Switch, Row, Col, Button } from 'antd';
import { ThunderboltOutlined, PlusOutlined, DeleteOutlined, SettingOutlined, DatabaseOutlined, LockOutlined } from '@ant-design/icons';
import { api } from '@/api/client';
import type { SpecInfo } from '@/types';

const ServerlessPage = () => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [selectedSpec, setSelectedSpec] = useState<SpecInfo | null>(null);
  const [form] = Form.useForm();
  const [envMode, setEnvMode] = useState<'form' | 'text'>('form');
  const [envText, setEnvText] = useState('');

  // Fetch specs with capacity
  const { data: specs, isLoading } = useQuery({
    queryKey: ['specs-capacity'],
    queryFn: async () => {
      const res = await api.specs.listWithCapacity();
      return res.data || [];
    },
  });

  // Fetch PVCs
  const { data: pvcs } = useQuery({
    queryKey: ['pvcs'],
    queryFn: async () => (await api.k8s.listPVCs()).data,
  });

  // Fetch default env
  useEffect(() => {
    if (isModalOpen) {
      api.config.getDefaultEnv().then((res) => {
        const envArray = Object.entries(res.data || {}).map(([key, value]) => ({ key, value }));
        form.setFieldValue('envVars', envArray);
      }).catch(() => form.setFieldValue('envVars', []));
    }
  }, [isModalOpen, form]);

  // Create endpoint mutation
  const createMutation = useMutation({
    mutationFn: (data: any) => api.endpoints.create(data),
    onSuccess: (_, variables) => {
      message.success('Endpoint created successfully');
      queryClient.invalidateQueries({ queryKey: ['endpoints'] });
      setIsModalOpen(false);
      form.resetFields();
      navigate(`/endpoints/${variables.endpoint}`);
    },
    onError: (error: any) => {
      message.error(error.response?.data?.error || 'Failed to create endpoint');
    },
  });

  const openCreateModal = (spec: SpecInfo) => {
    setSelectedSpec(spec);
    const maxGpu = parseInt(spec.resources?.gpu || '1') || 1;
    form.setFieldsValue({
      specName: spec.name,
      gpuCount: 1,
      maxGpuCount: maxGpu,
      replicas: 1,
      taskTimeout: 3600,
      maxPendingTasks: 100,
      shmSize: spec.resources?.shmSize || '1Gi',
      validateImage: true,
      enablePtrace: false,
      autoscaler: {
        minReplicas: 0,
        maxReplicas: 5,
        scaleUpThreshold: 1,
        scaleDownIdleTime: 300,
        scaleUpCooldown: 60,
        scaleDownCooldown: 120,
        priority: 100,
        enableDynamicPrio: true,
        highLoadThreshold: 10,
        priorityBoost: 50,
      },
    });
    setIsModalOpen(true);
  };

  const handleCreate = async () => {
    try {
      const values = await form.validateFields();
      const allValues = form.getFieldsValue(true);
      
      const data: any = {
        endpoint: values.name,
        image: values.image,
        imagePrefix: values.imagePrefix || undefined,
        specName: values.specName,
        gpuCount: values.gpuCount || 1,
        replicas: values.replicas,
        taskTimeout: values.taskTimeout || 0,
        maxPendingTasks: values.maxPendingTasks || undefined,
        shmSize: values.shmSize || undefined,
        validateImage: values.validateImage,
        enablePtrace: values.enablePtrace || false,
        minReplicas: allValues.autoscaler?.minReplicas,
        maxReplicas: allValues.autoscaler?.maxReplicas,
        scaleUpThreshold: allValues.autoscaler?.scaleUpThreshold,
        scaleDownIdleTime: allValues.autoscaler?.scaleDownIdleTime,
        scaleUpCooldown: allValues.autoscaler?.scaleUpCooldown,
        scaleDownCooldown: allValues.autoscaler?.scaleDownCooldown,
        priority: allValues.autoscaler?.priority,
        enableDynamicPrio: allValues.autoscaler?.enableDynamicPrio,
        highLoadThreshold: allValues.autoscaler?.highLoadThreshold,
        priorityBoost: allValues.autoscaler?.priorityBoost,
      };

      // Env vars - support both form and text mode
      const env: Record<string, string> = {};
      if (envMode === 'text') {
        envText.split('\n').filter(line => line.includes('=')).forEach(line => {
          const idx = line.indexOf('=');
          const key = line.slice(0, idx).trim();
          const value = line.slice(idx + 1).trim();
          if (key) env[key] = value;
        });
      } else {
        const envVars = allValues.envVars || [];
        envVars.filter((item: any) => item?.key).forEach((item: any) => { env[item.key] = item.value || ''; });
      }
      if (Object.keys(env).length > 0) data.env = env;

      // Volume mounts
      const volumeMounts = allValues.volumeMounts || [];
      if (volumeMounts.length > 0) {
        data.volumeMounts = volumeMounts.filter((vm: any) => vm?.pvcName && vm?.mountPath).map((vm: any) => ({ pvcName: vm.pvcName, mountPath: vm.mountPath }));
      }

      // Registry credential
      const regCred = allValues.registryCredential;
      if (regCred?.registry && regCred?.username && regCred?.password) {
        data.registryCredential = {
          registry: regCred.registry,
          username: regCred.username,
          password: regCred.password,
        };
      }

      createMutation.mutate(data);
    } catch (error) {
      console.error('Validation failed:', error);
    }
  };

  // Group specs by category
  const gpuSpecs = specs?.filter((s: SpecInfo) => s.category === 'gpu') || [];
  const cpuSpecs = specs?.filter((s: SpecInfo) => s.category === 'cpu') || [];

  if (isLoading) {
    return <div className="loading"><div className="spinner"></div></div>;
  }

  return (
    <>
      <div className="card mb-5">
        <div className="card-header">
          <h3>GPU Specs</h3>
          <span className="subtitle">Select a GPU spec to create endpoint</span>
        </div>
        <div className="card-body">
          <div className="specs-grid">
            {gpuSpecs.map((spec: SpecInfo) => (
              <div key={spec.name} className={`spec-card ${spec.capacity === 'sold_out' ? 'sold-out' : ''}`} onClick={() => openCreateModal(spec)}>
                <div className="spec-header">
                  <span className="spec-vram">Up to {spec.resources.gpu}× GPU</span>
                  <span className="spec-gpu">{spec.resources.gpuType}</span>
                  {spec.capacity && (
                    <span className={`capacity-badge ${spec.capacity}`}>
                      {spec.capacity === 'available' ? 'Available' : spec.capacity === 'limited' ? 'Limited' : 'Sold Out'}
                    </span>
                  )}
                </div>
                <div className="spec-desc">
                  {spec.displayName || spec.name}
                </div>
                <div className="spec-footer">
                  <span className="spec-resources">
                    {spec.resources.cpu} CPU • {spec.resources.memory}
                    {spec.spotPrice ? ` • $${spec.spotPrice.toFixed(2)}/hr` : ''}
                  </span>
                  <button className="btn btn-primary">
                    <PlusOutlined /> Create
                  </button>
                </div>
              </div>
            ))}
            {gpuSpecs.length === 0 && (
              <div className="empty-state">
                <ThunderboltOutlined style={{ fontSize: 48, opacity: 0.3 }} />
                <p>No GPU specs available</p>
              </div>
            )}
          </div>
        </div>
      </div>

      {cpuSpecs.length > 0 && (
        <div className="card">
          <div className="card-header">
            <h3>CPU Specs</h3>
          </div>
          <div className="card-body">
            <div className="specs-grid">
              {cpuSpecs.map((spec: SpecInfo) => (
                <div key={spec.name} className={`spec-card ${spec.capacity === 'sold_out' ? 'sold-out' : ''}`} onClick={() => openCreateModal(spec)}>
                  <div className="spec-header">
                    <span className="spec-vram">{spec.resources.cpu} CPU</span>
                    <span className="spec-gpu">{spec.resources.memory}</span>
                    {spec.capacity && (
                      <span className={`capacity-badge ${spec.capacity}`}>
                        {spec.capacity === 'available' ? 'Available' : spec.capacity === 'limited' ? 'Limited' : 'Sold Out'}
                      </span>
                    )}
                  </div>
                  <div className="spec-desc">{spec.displayName || spec.name}</div>
                  <div className="spec-footer">
                    <span className="spec-resources">
                      CPU Only{spec.spotPrice ? ` • $${spec.spotPrice.toFixed(2)}/hr` : ''}
                    </span>
                    <button className="btn btn-primary">
                      <PlusOutlined /> Create
                    </button>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* Create Modal */}
      <Modal
        title="Create Endpoint"
        open={isModalOpen}
        onOk={handleCreate}
        onCancel={() => setIsModalOpen(false)}
        confirmLoading={createMutation.isPending}
        okText="Create"
        width={700}
      >
        <Form form={form} layout="vertical" style={{ marginTop: 20 }} initialValues={{ autoscaler: { maxReplicas: 5 } }}>
          <Form.Item name="name" label="Endpoint Name" rules={[{ required: true, message: 'Please enter endpoint name' }, { pattern: /^[a-z0-9-]+$/, message: 'Only lowercase letters, numbers and hyphens' }]}>
            <Input placeholder="my-endpoint" />
          </Form.Item>

          <Form.Item name="image" label="Docker Image" rules={[{ required: true, message: 'Please enter docker image' }]}>
            <Input placeholder="your-registry/your-image:tag" addonAfter={
              <Form.Item name="validateImage" valuePropName="checked" noStyle>
                <Switch size="small" checkedChildren="Validate" unCheckedChildren="Skip" />
              </Form.Item>
            } />
          </Form.Item>

          <Form.Item name="imagePrefix" label="Image Prefix" tooltip="For auto image update detection">
            <Input placeholder="e.g., wavespeed/model:prefix-" />
          </Form.Item>

          <Form.Item name="specName" label="Hardware Spec">
            <Select disabled><Select.Option value={selectedSpec?.name}>{selectedSpec?.displayName || selectedSpec?.name}</Select.Option></Select>
          </Form.Item>

          {selectedSpec?.category === 'gpu' && (
            <Form.Item name="gpuCount" label="GPU Count" tooltip={`Max ${selectedSpec?.resources?.gpu || 1} GPUs. Resources scale with GPU count.`}>
              <InputNumber min={1} max={parseInt(selectedSpec?.resources?.gpu || '1') || 1} style={{ width: '100%' }} />
            </Form.Item>
          )}

          <Row gutter={16}>
            <Col span={8}><Form.Item name="replicas" label="Replicas"><InputNumber min={0} max={100} style={{ width: '100%' }} /></Form.Item></Col>
            <Col span={8}><Form.Item name="taskTimeout" label="Task Timeout (s)" tooltip="0 = default 3600s"><InputNumber min={0} style={{ width: '100%' }} /></Form.Item></Col>
            <Col span={8}><Form.Item name="maxPendingTasks" label="Max Pending"><InputNumber min={1} style={{ width: '100%' }} /></Form.Item></Col>
          </Row>

          <Row gutter={16}>
            <Col span={12}><Form.Item name="shmSize" label="Shared Memory" tooltip="e.g., 1Gi, 512Mi"><Input placeholder="Auto from spec" /></Form.Item></Col>
            <Col span={12}><Form.Item name="enablePtrace" label="Enable Ptrace" valuePropName="checked"><Switch /></Form.Item></Col>
          </Row>

          <Collapse ghost items={[
            { key: 'registry', label: <span><LockOutlined /> Registry Auth (Optional)</span>, children: (
              <>
                <Form.Item name={['registryCredential', 'registry']} label="Registry URL" tooltip="e.g., docker.io, ghcr.io, your-registry.com">
                  <Input placeholder="docker.io" />
                </Form.Item>
                <Row gutter={16}>
                  <Col span={12}><Form.Item name={['registryCredential', 'username']} label="Username"><Input placeholder="username" /></Form.Item></Col>
                  <Col span={12}><Form.Item name={['registryCredential', 'password']} label="Password"><Input.Password placeholder="password or token" /></Form.Item></Col>
                </Row>
              </>
            )},
            { key: 'autoscaler', label: <span><SettingOutlined /> AutoScaler Config</span>, children: (
              <>
                <Row gutter={16}>
                  <Col span={8}><Form.Item name={['autoscaler', 'minReplicas']} label="Min Replicas"><InputNumber min={0} style={{ width: '100%' }} /></Form.Item></Col>
                  <Col span={8}><Form.Item name={['autoscaler', 'maxReplicas']} label="Max Replicas"><InputNumber min={1} style={{ width: '100%' }} /></Form.Item></Col>
                  <Col span={8}><Form.Item name={['autoscaler', 'scaleUpThreshold']} label="Scale Up Threshold"><InputNumber min={1} style={{ width: '100%' }} /></Form.Item></Col>
                </Row>
                <Row gutter={16}>
                  <Col span={8}><Form.Item name={['autoscaler', 'scaleDownIdleTime']} label="Idle Time (s)"><InputNumber min={0} style={{ width: '100%' }} /></Form.Item></Col>
                  <Col span={8}><Form.Item name={['autoscaler', 'scaleUpCooldown']} label="Up Cooldown (s)"><InputNumber min={0} style={{ width: '100%' }} /></Form.Item></Col>
                  <Col span={8}><Form.Item name={['autoscaler', 'scaleDownCooldown']} label="Down Cooldown (s)"><InputNumber min={0} style={{ width: '100%' }} /></Form.Item></Col>
                </Row>
                <Row gutter={16}>
                  <Col span={8}><Form.Item name={['autoscaler', 'priority']} label="Priority"><InputNumber min={0} style={{ width: '100%' }} /></Form.Item></Col>
                  <Col span={8}><Form.Item name={['autoscaler', 'enableDynamicPrio']} label="Dynamic Priority" valuePropName="checked"><Switch /></Form.Item></Col>
                </Row>
                <Row gutter={16}>
                  <Col span={8}><Form.Item name={['autoscaler', 'highLoadThreshold']} label="High Load Threshold"><InputNumber min={0} style={{ width: '100%' }} /></Form.Item></Col>
                  <Col span={8}><Form.Item name={['autoscaler', 'priorityBoost']} label="Priority Boost"><InputNumber min={0} style={{ width: '100%' }} /></Form.Item></Col>
                </Row>
              </>
            )},
            { key: 'env', label: (
              <div style={{ display: 'flex', justifyContent: 'space-between', width: '100%', alignItems: 'center' }}>
                <span><SettingOutlined /> Environment Variables</span>
                <Button type="link" size="small" onClick={(e) => {
                  e.stopPropagation();
                  if (envMode === 'form') {
                    const envVars = form.getFieldValue('envVars') || [];
                    const text = envVars.filter((v: any) => v?.key).map((v: any) => `${v.key}=${v.value || ''}`).join('\n');
                    setEnvText(text);
                  } else {
                    const vars = envText.split('\n').filter(line => line.includes('=')).map(line => {
                      const idx = line.indexOf('=');
                      return { key: line.slice(0, idx).trim(), value: line.slice(idx + 1).trim() };
                    });
                    form.setFieldValue('envVars', vars.length ? vars : [{ key: '', value: '' }]);
                  }
                  setEnvMode(envMode === 'form' ? 'text' : 'form');
                }}>{envMode === 'form' ? 'Switch to Text' : 'Switch to Form'}</Button>
              </div>
            ), children: envMode === 'text' ? (
              <Input.TextArea
                value={envText}
                onChange={e => setEnvText(e.target.value)}
                placeholder="KEY=value&#10;ANOTHER_KEY=another value"
                rows={6}
                style={{ fontFamily: 'monospace' }}
              />
            ) : (
              <Form.List name="envVars">
                {(fields, { add, remove }) => (
                  <>
                    {fields.map((field) => (
                      <Row gutter={8} key={field.key} style={{ marginBottom: 8 }}>
                        <Col span={10}><Form.Item {...field} name={[field.name, 'key']} style={{ marginBottom: 0 }}><Input placeholder="KEY" /></Form.Item></Col>
                        <Col span={12}><Form.Item {...field} name={[field.name, 'value']} style={{ marginBottom: 0 }}><Input placeholder="value" /></Form.Item></Col>
                        <Col span={2}><Button type="text" danger icon={<DeleteOutlined />} onClick={() => remove(field.name)} /></Col>
                      </Row>
                    ))}
                    <Button type="dashed" onClick={() => add()} block icon={<PlusOutlined />}>Add Variable</Button>
                  </>
                )}
              </Form.List>
            )},
            { key: 'volumes', label: <span><DatabaseOutlined /> Volume Mounts</span>, children: (
              <Form.List name="volumeMounts">
                {(fields, { add, remove }) => (
                  <>
                    {fields.map((field) => (
                      <Row gutter={8} key={field.key} style={{ marginBottom: 8 }}>
                        <Col span={10}><Form.Item {...field} name={[field.name, 'pvcName']} style={{ marginBottom: 0 }}><Select placeholder="Select PVC" options={pvcs?.map((p: any) => ({ value: p.name, label: p.name }))} /></Form.Item></Col>
                        <Col span={12}><Form.Item {...field} name={[field.name, 'mountPath']} style={{ marginBottom: 0 }}><Input placeholder="/data/models" /></Form.Item></Col>
                        <Col span={2}><Button type="text" danger icon={<DeleteOutlined />} onClick={() => remove(field.name)} /></Col>
                      </Row>
                    ))}
                    <Button type="dashed" onClick={() => add()} block icon={<PlusOutlined />}>Add Volume</Button>
                  </>
                )}
              </Form.List>
            )},
          ]} />
        </Form>
      </Modal>
    </>
  );
};

export default ServerlessPage;
