import React from 'react';
import { Tag, Tooltip, Space } from 'antd';
import { CheckCircleOutlined, ExclamationCircleOutlined, CloseCircleOutlined } from '@ant-design/icons';
import type { SpotStatus, SpotCapacity } from '@/types';

interface SpotCapacityIndicatorProps {
  status: SpotStatus | null | undefined;
  showDetails?: boolean;
  size?: 'small' | 'default';
}

const capacityConfig: Record<SpotCapacity, { color: string; icon: React.ReactNode; label: string }> = {
  AVAILABLE: {
    color: 'success',
    icon: <CheckCircleOutlined />,
    label: 'Available',
  },
  LIMITED: {
    color: 'warning',
    icon: <ExclamationCircleOutlined />,
    label: 'Limited',
  },
  CONSTRAINED: {
    color: 'error',
    icon: <CloseCircleOutlined />,
    label: 'Constrained',
  },
};

export const SpotCapacityIndicator: React.FC<SpotCapacityIndicatorProps> = ({
  status,
  showDetails = false,
  size = 'default',
}) => {
  if (!status) {
    return null;
  }

  const config = capacityConfig[status.capacity] || capacityConfig.CONSTRAINED;

  const tooltipContent = (
    <div>
      <div><strong>Spot Capacity:</strong> {config.label}</div>
      <div><strong>Score:</strong> {status.score}/10</div>
      <div><strong>Price:</strong> ${status.price.toFixed(4)}/hr</div>
      <div><strong>Instance:</strong> {status.instanceType}</div>
    </div>
  );

  if (showDetails) {
    return (
      <Space direction="vertical" size="small">
        <Tag icon={config.icon} color={config.color}>
          Spot: {config.label}
        </Tag>
        <div style={{ fontSize: size === 'small' ? '12px' : '14px', color: '#666' }}>
          <div>Score: {status.score}/10</div>
          <div>Price: ${status.price.toFixed(4)}/hr</div>
          <div>Instance: {status.instanceType}</div>
        </div>
      </Space>
    );
  }

  return (
    <Tooltip title={tooltipContent}>
      <Tag icon={config.icon} color={config.color} style={{ cursor: 'help' }}>
        Spot: {config.label}
      </Tag>
    </Tooltip>
  );
};

export default SpotCapacityIndicator;
