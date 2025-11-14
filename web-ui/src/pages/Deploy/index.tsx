import { useState } from 'react';
import { useQuery, useMutation } from '@tanstack/react-query';
import {
  Card,
  Form,
  Input,
  Select,
  InputNumber,
  Button,
  message,
  Spin,
  Row,
  Col,
  Typography,
  Divider,
  Modal,
  Collapse,
  Switch,
} from 'antd';
import { RocketOutlined, EyeOutlined, ThunderboltOutlined } from '@ant-design/icons';
import { api } from '@/api/client';
import type { DeployRequest } from '@/types';

const { Title, Paragraph, Text } = Typography;
const { TextArea } = Input;

// Validate and normalize Kubernetes resource name (DNS-1123 label)
const normalizeK8sName = (name: string): string => {
  return name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9-]/g, '-') // Replace invalid chars with '-'
    .replace(/^-+|-+$/g, '')     // Remove leading/trailing '-'
    .replace(/-+/g, '-')          // Replace multiple '-' with single '-'
    .substring(0, 63);            // Limit to 63 chars
};

const validateK8sName = (_: any, value: string) => {
  if (!value || !value.trim()) {
    return Promise.reject(new Error('Endpoint name is required'));
  }

  const normalized = normalizeK8sName(value);

  if (normalized.length === 0) {
    return Promise.reject(new Error('Endpoint name contains only invalid characters'));
  }

  if (!/^[a-z0-9]/.test(normalized)) {
    return Promise.reject(new Error('Endpoint name must start with a lowercase letter or number'));
  }

  if (!/[a-z0-9]$/.test(normalized)) {
    return Promise.reject(new Error('Endpoint name must end with a lowercase letter or number'));
  }

  return Promise.resolve();
};

const DeployPage = () => {
  const [form] = Form.useForm();
  const [selectedSpec, setSelectedSpec] = useState<string>('');
  const [yamlPreview, setYamlPreview] = useState<string>('');
  const [previewVisible, setPreviewVisible] = useState(false);

  // Fetch specs
  const { data: specs, isLoading: specsLoading } = useQuery({
    queryKey: ['specs'],
    queryFn: async () => {
      const response = await api.specs.list();
      return response.data;
    },
  });

  // Get selected spec details
  const selectedSpecData = specs?.find((s) => s.name === selectedSpec);

  // Deploy mutation
  const deployMutation = useMutation({
    mutationFn: async (values: DeployRequest) => {
      const response = await api.apps.deploy(values);
      return response.data;
    },
    onSuccess: () => {
      message.success('Application deployed successfully!');
      form.resetFields();
    },
    onError: (error: any) => {
      message.error(error.response?.data?.error || 'Failed to deploy application');
    },
  });

  // Preview YAML mutation
  const previewMutation = useMutation({
    mutationFn: async (values: DeployRequest) => {
      const response = await api.apps.previewYaml(values);
      return response.data;
    },
    onSuccess: (data) => {
      setYamlPreview(data);
      setPreviewVisible(true);
    },
    onError: (error: any) => {
      message.error(error.response?.data?.error || 'Failed to preview YAML');
    },
  });

  const handleDeploy = (values: any) => {
    const normalizedEndpoint = normalizeK8sName(values.endpoint);
    const deployData: DeployRequest = {
      endpoint: normalizedEndpoint,
      specName: values.specName,
      image: values.image,
      replicas: values.replicas || 1,
      taskTimeout: values.taskTimeout || 0,
    };

    // Add autoscaler config if provided
    if (values.autoscaler) {
      if (values.autoscaler.minReplicas !== undefined) {
        deployData.minReplicas = values.autoscaler.minReplicas;
      }
      if (values.autoscaler.maxReplicas !== undefined && values.autoscaler.maxReplicas > 0) {
        deployData.maxReplicas = values.autoscaler.maxReplicas;
      }
      if (values.autoscaler.scaleUpThreshold !== undefined && values.autoscaler.scaleUpThreshold > 0) {
        deployData.scaleUpThreshold = values.autoscaler.scaleUpThreshold;
      }
      if (values.autoscaler.scaleDownIdleTime !== undefined && values.autoscaler.scaleDownIdleTime > 0) {
        deployData.scaleDownIdleTime = values.autoscaler.scaleDownIdleTime;
      }
      if (values.autoscaler.scaleUpCooldown !== undefined && values.autoscaler.scaleUpCooldown > 0) {
        deployData.scaleUpCooldown = values.autoscaler.scaleUpCooldown;
      }
      if (values.autoscaler.scaleDownCooldown !== undefined && values.autoscaler.scaleDownCooldown > 0) {
        deployData.scaleDownCooldown = values.autoscaler.scaleDownCooldown;
      }
      if (values.autoscaler.priority !== undefined && values.autoscaler.priority > 0) {
        deployData.priority = values.autoscaler.priority;
      }
      if (values.autoscaler.enableDynamicPrio !== undefined) {
        deployData.enableDynamicPrio = values.autoscaler.enableDynamicPrio;
      }
      if (values.autoscaler.highLoadThreshold !== undefined && values.autoscaler.highLoadThreshold > 0) {
        deployData.highLoadThreshold = values.autoscaler.highLoadThreshold;
      }
      if (values.autoscaler.priorityBoost !== undefined && values.autoscaler.priorityBoost > 0) {
        deployData.priorityBoost = values.autoscaler.priorityBoost;
      }
    }

    deployMutation.mutate(deployData);
  };

  const handlePreview = () => {
    form.validateFields().then((values) => {
      const normalizedEndpoint = normalizeK8sName(values.endpoint);
      const previewData: DeployRequest = {
        endpoint: normalizedEndpoint,
        specName: values.specName,
        image: values.image,
        replicas: values.replicas || 1,
        taskTimeout: values.taskTimeout || 0,
      };

      // Add autoscaler config if provided
      if (values.autoscaler) {
        if (values.autoscaler.minReplicas !== undefined) {
          previewData.minReplicas = values.autoscaler.minReplicas;
        }
        if (values.autoscaler.maxReplicas !== undefined && values.autoscaler.maxReplicas > 0) {
          previewData.maxReplicas = values.autoscaler.maxReplicas;
        }
        if (values.autoscaler.scaleUpThreshold !== undefined && values.autoscaler.scaleUpThreshold > 0) {
          previewData.scaleUpThreshold = values.autoscaler.scaleUpThreshold;
        }
        if (values.autoscaler.scaleDownIdleTime !== undefined && values.autoscaler.scaleDownIdleTime > 0) {
          previewData.scaleDownIdleTime = values.autoscaler.scaleDownIdleTime;
        }
        if (values.autoscaler.scaleUpCooldown !== undefined && values.autoscaler.scaleUpCooldown > 0) {
          previewData.scaleUpCooldown = values.autoscaler.scaleUpCooldown;
        }
        if (values.autoscaler.scaleDownCooldown !== undefined && values.autoscaler.scaleDownCooldown > 0) {
          previewData.scaleDownCooldown = values.autoscaler.scaleDownCooldown;
        }
        if (values.autoscaler.priority !== undefined && values.autoscaler.priority > 0) {
          previewData.priority = values.autoscaler.priority;
        }
        if (values.autoscaler.enableDynamicPrio !== undefined) {
          previewData.enableDynamicPrio = values.autoscaler.enableDynamicPrio;
        }
        if (values.autoscaler.highLoadThreshold !== undefined && values.autoscaler.highLoadThreshold > 0) {
          previewData.highLoadThreshold = values.autoscaler.highLoadThreshold;
        }
        if (values.autoscaler.priorityBoost !== undefined && values.autoscaler.priorityBoost > 0) {
          previewData.priorityBoost = values.autoscaler.priorityBoost;
        }
      }

      previewMutation.mutate(previewData);
    });
  };

  return (
    <div>
      <Title level={2}>
        <RocketOutlined /> Deploy New Application
      </Title>
      <Paragraph type="secondary">
        Deploy a new Kubernetes application with your specified configuration
      </Paragraph>

      <Spin spinning={specsLoading}>
        <Row gutter={24}>
          <Col xs={24} lg={12}>
            <Card title="Deployment Configuration" bordered={false}>
              <Form
                form={form}
                layout="vertical"
                onFinish={handleDeploy}
                initialValues={{ 
                  replicas: 1, 
                  taskTimeout: 0,
                  autoscaler: {
                    maxReplicas: 1  // Default maxReplicas to 1, not 0
                  }
                }}
              >
                <Form.Item
                  name="endpoint"
                  label="Endpoint Name"
                  rules={[
                    { required: true, message: 'Please enter endpoint name' },
                    { validator: validateK8sName }
                  ]}
                  tooltip="Must be lowercase alphanumeric with hyphens, start/end with alphanumeric (max 63 chars)"
                >
                  <Input
                    placeholder="e.g., my-app"
                    onBlur={(e) => {
                      const normalized = normalizeK8sName(e.target.value);
                      if (normalized !== e.target.value) {
                        form.setFieldValue('endpoint', normalized);
                      }
                    }}
                  />
                </Form.Item>

                <Form.Item
                  name="specName"
                  label="GPU Spec"
                  rules={[{ required: true, message: 'Please select a spec' }]}
                >
                  <Select
                    placeholder="Select a GPU spec"
                    onChange={setSelectedSpec}
                    options={specs?.map((spec) => ({
                      label: `${spec.displayName} (${spec.name})`,
                      value: spec.name,
                    }))}
                  />
                </Form.Item>

                <Form.Item
                  name="image"
                  label="Docker Image"
                  rules={[{ required: true, message: 'Please enter docker image' }]}
                >
                  <Input placeholder="e.g., wavespeed/model-deploy:latest" />
                </Form.Item>

                <Row gutter={16}>
                  <Col span={12}>
                    <Form.Item
                      name="replicas"
                      label="Replicas"
                      rules={[{ required: true, message: 'Please enter replicas' }]}
                    >
                      <InputNumber min={1} max={10} style={{ width: '100%' }} />
                    </Form.Item>
                  </Col>
                  <Col span={12}>
                    <Form.Item
                      name="taskTimeout"
                      label="Task Timeout (seconds)"
                      tooltip="0 = use global default (3600s)"
                    >
                      <InputNumber min={0} step={300} style={{ width: '100%' }} />
                    </Form.Item>
                  </Col>
                </Row>

                <Divider />

                <Collapse
                  items={[
                    {
                      key: 'autoscaler',
                      label: (
                        <span>
                          <ThunderboltOutlined /> AutoScaler Configuration (Optional)
                        </span>
                      ),
                      children: (
                        <>
                          <Paragraph type="secondary">
                            Configure automatic scaling based on queue depth. Leave blank to use defaults or configure later.
                          </Paragraph>

                          <Title level={5}>Replica Limits</Title>
                          <Row gutter={16}>
                            <Col span={12}>
                              <Form.Item
                                name={['autoscaler', 'minReplicas']}
                                label="Min Replicas"
                                tooltip="Minimum number of replicas (0 to scale to zero when idle)"
                              >
                                <InputNumber min={0} max={100} style={{ width: '100%' }} placeholder="0" />
                              </Form.Item>
                            </Col>
                            <Col span={12}>
                              <Form.Item
                                name={['autoscaler', 'maxReplicas']}
                                label="Max Replicas"
                                tooltip="Maximum number of replicas to scale up to"
                              >
                                <InputNumber min={1} max={100} style={{ width: '100%' }} placeholder="1" />
                              </Form.Item>
                            </Col>
                          </Row>

                          <Title level={5}>Scaling Triggers</Title>
                          <Form.Item
                            name={['autoscaler', 'scaleUpThreshold']}
                            label="Scale Up Threshold"
                            tooltip="Number of pending tasks to trigger scale up"
                          >
                            <InputNumber min={1} max={1000} style={{ width: '100%' }} placeholder="1" />
                          </Form.Item>
                          <Form.Item
                            name={['autoscaler', 'scaleDownIdleTime']}
                            label="Scale Down Idle Time (seconds)"
                            tooltip="Time without tasks before scaling down"
                          >
                            <InputNumber min={0} max={3600} style={{ width: '100%' }} placeholder="300" />
                          </Form.Item>

                          <Title level={5}>Cooldown Periods</Title>
                          <Row gutter={16}>
                            <Col span={12}>
                              <Form.Item
                                name={['autoscaler', 'scaleUpCooldown']}
                                label="Scale Up Cooldown (seconds)"
                                tooltip="Minimum time between scale up operations"
                              >
                                <InputNumber min={0} max={3600} style={{ width: '100%' }} placeholder="30" />
                              </Form.Item>
                            </Col>
                            <Col span={12}>
                              <Form.Item
                                name={['autoscaler', 'scaleDownCooldown']}
                                label="Scale Down Cooldown (seconds)"
                                tooltip="Minimum time between scale down operations"
                              >
                                <InputNumber min={0} max={3600} style={{ width: '100%' }} placeholder="60" />
                              </Form.Item>
                            </Col>
                          </Row>

                          <Title level={5}>Priority</Title>
                          <Form.Item
                            name={['autoscaler', 'priority']}
                            label="Priority (0-100)"
                            tooltip="Higher priority endpoints get resources first when competing"
                          >
                            <InputNumber min={0} max={100} style={{ width: '100%' }} placeholder="50" />
                          </Form.Item>
                          <Form.Item
                            name={['autoscaler', 'enableDynamicPrio']}
                            label="Enable Dynamic Priority Boost"
                            valuePropName="checked"
                            tooltip="Automatically increase priority when queue is long"
                          >
                            <Switch />
                          </Form.Item>
                          <Row gutter={16}>
                            <Col span={12}>
                              <Form.Item
                                name={['autoscaler', 'highLoadThreshold']}
                                label="High Load Threshold"
                                tooltip="Queue length to trigger priority boost"
                              >
                                <InputNumber min={1} max={1000} style={{ width: '100%' }} placeholder="10" />
                              </Form.Item>
                            </Col>
                            <Col span={12}>
                              <Form.Item
                                name={['autoscaler', 'priorityBoost']}
                                label="Priority Boost"
                                tooltip="Priority increase amount when high load"
                              >
                                <InputNumber min={0} max={100} style={{ width: '100%' }} placeholder="20" />
                              </Form.Item>
                            </Col>
                          </Row>
                        </>
                      ),
                    },
                  ]}
                  style={{ marginBottom: 16 }}
                />

                <Form.Item>
                  <Button
                    type="primary"
                    htmlType="submit"
                    icon={<RocketOutlined />}
                    loading={deployMutation.isPending}
                    block
                    size="large"
                  >
                    Deploy Application
                  </Button>
                </Form.Item>

                <Form.Item>
                  <Button
                    icon={<EyeOutlined />}
                    onClick={handlePreview}
                    loading={previewMutation.isPending}
                    block
                  >
                    Preview YAML
                  </Button>
                </Form.Item>
              </Form>
            </Card>
          </Col>

          <Col xs={24} lg={12}>
            {selectedSpecData && (
              <Card title="Spec Details" bordered={false}>
                <Title level={5}>{selectedSpecData.displayName}</Title>
                <Paragraph>
                  <Text strong>Category:</Text> {selectedSpecData.category}
                </Paragraph>

                <Divider />

                <Title level={5}>Resources</Title>
                <Paragraph>
                  {selectedSpecData.resources.gpu && (
                    <>
                      <Text strong>GPU:</Text> {selectedSpecData.resources.gpu} ×{' '}
                      {selectedSpecData.resources.gpuType}
                      <br />
                    </>
                  )}
                  {selectedSpecData.resources.cpu && (
                    <>
                      <Text strong>CPU:</Text> {selectedSpecData.resources.cpu}
                      <br />
                    </>
                  )}
                  <Text strong>Memory:</Text> {selectedSpecData.resources.memory}
                  <br />
                  {selectedSpecData.resources.disk && (
                    <>
                      <Text strong>Disk:</Text> {selectedSpecData.resources.disk}GB
                      <br />
                    </>
                  )}
                  {selectedSpecData.resources.ephemeralStorage && (
                    <>
                      <Text strong>Ephemeral Storage:</Text>{' '}
                      {selectedSpecData.resources.ephemeralStorage}GB
                    </>
                  )}
                </Paragraph>
              </Card>
            )}
          </Col>
        </Row>
      </Spin>

      <Modal
        title="Deployment YAML Preview"
        open={previewVisible}
        onCancel={() => setPreviewVisible(false)}
        footer={null}
        width={800}
      >
        <TextArea
          value={yamlPreview}
          readOnly
          autoSize={{ minRows: 20, maxRows: 40 }}
          style={{ fontFamily: 'monospace' }}
        />
      </Modal>
    </div>
  );
};

export default DeployPage;
