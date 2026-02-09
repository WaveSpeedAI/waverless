import React from 'react';
import { Card, Row, Col, Statistic, Table, Tag, Typography, Space, Tooltip, Empty } from 'antd';
import {
  CheckCircleOutlined,
  ClockCircleOutlined,
  ExclamationCircleOutlined,
  CloseCircleOutlined,
  LoadingOutlined,
} from '@ant-design/icons';
import type { EndpointStatusSummary as StatusSummaryType, WorkerPendingDetail, WorkerFailureDetail, PendingPhase } from '@/types';
import { SpotCapacityIndicator } from './SpotCapacityIndicator';

const { Text } = Typography;

interface EndpointStatusSummaryProps {
  summary: StatusSummaryType | null | undefined;
  loading?: boolean;
}

// Status color mapping
const statusColors: Record<string, string> = {
  ONLINE: 'success',
  PENDING: 'processing',
  STARTING: 'processing',
  FAILED: 'error',
  OFFLINE: 'default',
  DRAINING: 'warning',
};

// Pending phase display config
const phaseConfig: Record<PendingPhase, { color: string; icon: React.ReactNode; label: string }> = {
  SCHEDULING: {
    color: 'blue',
    icon: <ClockCircleOutlined />,
    label: 'Scheduling',
  },
  WAITING_NODE: {
    color: 'orange',
    icon: <LoadingOutlined />,
    label: 'Waiting for Node',
  },
  PULLING_IMAGE: {
    color: 'cyan',
    icon: <LoadingOutlined />,
    label: 'Pulling Image',
  },
  INITIALIZING: {
    color: 'purple',
    icon: <LoadingOutlined />,
    label: 'Initializing',
  },
};

// Pending details table columns
const pendingColumns = [
  {
    title: 'Worker',
    dataIndex: 'workerId',
    key: 'workerId',
    width: 150,
    ellipsis: true,
    render: (text: string) => (
      <Tooltip title={text}>
        <Text code style={{ fontSize: '12px' }}>{text.slice(-12)}</Text>
      </Tooltip>
    ),
  },
  {
    title: 'Phase',
    dataIndex: 'phase',
    key: 'phase',
    width: 140,
    render: (phase: PendingPhase) => {
      const config = phaseConfig[phase] || { color: 'default', icon: null, label: phase };
      return (
        <Tag icon={config.icon} color={config.color}>
          {config.label}
        </Tag>
      );
    },
  },
  {
    title: 'Reason',
    dataIndex: 'reason',
    key: 'reason',
    ellipsis: true,
    render: (text: string) => text || '-',
  },
  {
    title: 'Since',
    dataIndex: 'since',
    key: 'since',
    width: 100,
    render: (time: string) => {
      const date = new Date(time);
      const now = new Date();
      const diffMs = now.getTime() - date.getTime();
      const diffMins = Math.floor(diffMs / 60000);
      if (diffMins < 1) return 'Just now';
      if (diffMins < 60) return `${diffMins}m ago`;
      const diffHours = Math.floor(diffMins / 60);
      if (diffHours < 24) return `${diffHours}h ago`;
      return `${Math.floor(diffHours / 24)}d ago`;
    },
  },
];

// Failure details table columns
const failureColumns = [
  {
    title: 'Worker',
    dataIndex: 'workerId',
    key: 'workerId',
    width: 150,
    ellipsis: true,
    render: (text: string) => (
      <Tooltip title={text}>
        <Text code style={{ fontSize: '12px' }}>{text.slice(-12)}</Text>
      </Tooltip>
    ),
  },
  {
    title: 'Type',
    dataIndex: 'failureType',
    key: 'failureType',
    width: 140,
    render: (type: string) => (
      <Tag color="error">{type.replace(/_/g, ' ')}</Tag>
    ),
  },
  {
    title: 'Suggestion',
    dataIndex: 'suggestion',
    key: 'suggestion',
    ellipsis: true,
    render: (text: string) => (
      <Tooltip title={text}>
        <Text type="secondary">{text}</Text>
      </Tooltip>
    ),
  },
];

export const EndpointStatusSummary: React.FC<EndpointStatusSummaryProps> = ({
  summary,
  loading = false,
}) => {
  if (!summary) {
    return (
      <Card title="Status Summary" loading={loading}>
        <Empty description="No status summary available" />
      </Card>
    );
  }

  const { totalWorkers, workersByStatus, workersByPhase, pendingDetails, failureDetails, spotCapacity } = summary;

  // Calculate status counts
  const onlineCount = workersByStatus?.ONLINE || 0;
  const pendingCount = (workersByStatus?.PENDING || 0) + (workersByStatus?.STARTING || 0);
  const failedCount = workersByStatus?.FAILED || 0;
  const offlineCount = workersByStatus?.OFFLINE || 0;

  return (
    <Card title="Status Summary" loading={loading}>
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        {/* Worker Status Overview */}
        <Row gutter={16}>
          <Col span={6}>
            <Statistic
              title="Total Workers"
              value={totalWorkers}
              prefix={<CheckCircleOutlined />}
            />
          </Col>
          <Col span={6}>
            <Statistic
              title="Online"
              value={onlineCount}
              valueStyle={{ color: '#52c41a' }}
              prefix={<CheckCircleOutlined />}
            />
          </Col>
          <Col span={6}>
            <Statistic
              title="Pending"
              value={pendingCount}
              valueStyle={{ color: '#1890ff' }}
              prefix={<ClockCircleOutlined />}
            />
          </Col>
          <Col span={6}>
            <Statistic
              title="Failed"
              value={failedCount}
              valueStyle={{ color: '#ff4d4f' }}
              prefix={<CloseCircleOutlined />}
            />
          </Col>
        </Row>

        {/* Spot Capacity (if available) */}
        {spotCapacity && (
          <div>
            <Text strong>Spot Capacity: </Text>
            <SpotCapacityIndicator status={spotCapacity} showDetails />
          </div>
        )}

        {/* Pending Phase Breakdown */}
        {pendingCount > 0 && workersByPhase && Object.keys(workersByPhase).length > 0 && (
          <div>
            <Text strong>Pending Phases: </Text>
            <Space wrap style={{ marginTop: 8 }}>
              {Object.entries(workersByPhase).map(([phase, count]) => {
                const config = phaseConfig[phase as PendingPhase] || { color: 'default', label: phase };
                return (
                  <Tag key={phase} color={config.color}>
                    {config.label}: {count}
                  </Tag>
                );
              })}
            </Space>
          </div>
        )}

        {/* Pending Details Table */}
        {pendingDetails && pendingDetails.length > 0 && (
          <div>
            <Text strong style={{ display: 'block', marginBottom: 8 }}>
              <ExclamationCircleOutlined style={{ marginRight: 8, color: '#1890ff' }} />
              Pending Workers ({pendingDetails.length})
            </Text>
            <Table
              dataSource={pendingDetails}
              columns={pendingColumns}
              rowKey="workerId"
              size="small"
              pagination={false}
              scroll={{ y: 200 }}
            />
          </div>
        )}

        {/* Failure Details Table */}
        {failureDetails && failureDetails.length > 0 && (
          <div>
            <Text strong style={{ display: 'block', marginBottom: 8 }}>
              <CloseCircleOutlined style={{ marginRight: 8, color: '#ff4d4f' }} />
              Failed Workers ({failureDetails.length})
            </Text>
            <Table
              dataSource={failureDetails}
              columns={failureColumns}
              rowKey="workerId"
              size="small"
              pagination={false}
              scroll={{ y: 200 }}
            />
          </div>
        )}

        {/* Last Updated */}
        <Text type="secondary" style={{ fontSize: '12px' }}>
          Last updated: {new Date(summary.lastUpdated).toLocaleString()}
        </Text>
      </Space>
    </Card>
  );
};

export default EndpointStatusSummary;
