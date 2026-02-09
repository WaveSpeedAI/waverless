import React, { useState, useEffect } from 'react';
import { Card, Timeline, Tag, Typography, Space, Empty, Spin, Button, Tooltip } from 'antd';
import {
  CheckCircleOutlined,
  ClockCircleOutlined,
  CloseCircleOutlined,
  SyncOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import type { StatusEvent, StatusEventType } from '@/types';
import { api } from '@/api/client';
import { SpotCapacityIndicator } from './SpotCapacityIndicator';

const { Text } = Typography;

interface StatusTimelineProps {
  endpoint: string;
  workerId?: string;
  limit?: number;
  autoRefresh?: boolean;
  refreshInterval?: number;
}

// Event type display config
const eventTypeConfig: Record<StatusEventType, { color: string; icon: React.ReactNode; label: string }> = {
  STATUS_CHANGE: {
    color: 'blue',
    icon: <SyncOutlined />,
    label: 'Status Change',
  },
  PHASE_CHANGE: {
    color: 'cyan',
    icon: <ClockCircleOutlined />,
    label: 'Phase Change',
  },
  FAILURE: {
    color: 'red',
    icon: <CloseCircleOutlined />,
    label: 'Failure',
  },
  RECOVERY: {
    color: 'green',
    icon: <CheckCircleOutlined />,
    label: 'Recovery',
  },
};

// Status color mapping
const statusColors: Record<string, string> = {
  ONLINE: 'success',
  PENDING: 'processing',
  STARTING: 'processing',
  FAILED: 'error',
  OFFLINE: 'default',
  DRAINING: 'warning',
};

export const StatusTimeline: React.FC<StatusTimelineProps> = ({
  endpoint,
  workerId,
  limit = 20,
  autoRefresh = false,
  refreshInterval = 30000,
}) => {
  const [events, setEvents] = useState<StatusEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchEvents = async () => {
    try {
      setLoading(true);
      setError(null);

      let response;
      if (workerId) {
        response = await api.statusEvents.listByWorker(workerId, { limit });
      } else {
        response = await api.statusEvents.listByEndpoint(endpoint, { limit });
      }

      setEvents(response.data || []);
    } catch (err: any) {
      setError(err.message || 'Failed to load status events');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchEvents();

    if (autoRefresh) {
      const interval = setInterval(fetchEvents, refreshInterval);
      return () => clearInterval(interval);
    }
  }, [endpoint, workerId, limit, autoRefresh, refreshInterval]);

  const formatTime = (time: string) => {
    const date = new Date(time);
    return date.toLocaleString();
  };

  const formatRelativeTime = (time: string) => {
    const date = new Date(time);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    if (diffMins < 1) return 'Just now';
    if (diffMins < 60) return `${diffMins}m ago`;
    const diffHours = Math.floor(diffMins / 60);
    if (diffHours < 24) return `${diffHours}h ago`;
    return `${Math.floor(diffHours / 24)}d ago`;
  };

  const renderEventContent = (event: StatusEvent) => {
    const config = eventTypeConfig[event.eventType] || eventTypeConfig.STATUS_CHANGE;

    return (
      <Space direction="vertical" size="small" style={{ width: '100%' }}>
        <Space>
          <Tag color={config.color}>{config.label}</Tag>
          {event.oldStatus && (
            <>
              <Tag color={statusColors[event.oldStatus] || 'default'}>{event.oldStatus}</Tag>
              <span>→</span>
            </>
          )}
          <Tag color={statusColors[event.newStatus] || 'default'}>{event.newStatus}</Tag>
        </Space>

        {event.phase && (
          <Text type="secondary">Phase: {event.phase}</Text>
        )}

        {event.reason && (
          <Text type="secondary">Reason: {event.reason}</Text>
        )}

        {event.message && (
          <Tooltip title={event.message}>
            <Text type="secondary" ellipsis style={{ maxWidth: 300 }}>
              {event.message}
            </Text>
          </Tooltip>
        )}

        {event.spotStatus && (
          <SpotCapacityIndicator status={event.spotStatus} />
        )}

        <Space>
          <Text type="secondary" style={{ fontSize: '12px' }}>
            Worker: {event.workerId.slice(-12)}
          </Text>
          <Text type="secondary" style={{ fontSize: '12px' }}>
            {formatRelativeTime(event.createdAt)}
          </Text>
        </Space>
      </Space>
    );
  };

  const getTimelineItemColor = (event: StatusEvent): string => {
    switch (event.eventType) {
      case 'FAILURE':
        return 'red';
      case 'RECOVERY':
        return 'green';
      case 'PHASE_CHANGE':
        return 'blue';
      default:
        return 'gray';
    }
  };

  if (loading && events.length === 0) {
    return (
      <Card
        title="Status Timeline"
        extra={
          <Button icon={<ReloadOutlined />} onClick={fetchEvents} size="small">
            Refresh
          </Button>
        }
      >
        <div style={{ textAlign: 'center', padding: '20px' }}>
          <Spin />
        </div>
      </Card>
    );
  }

  if (error) {
    return (
      <Card
        title="Status Timeline"
        extra={
          <Button icon={<ReloadOutlined />} onClick={fetchEvents} size="small">
            Refresh
          </Button>
        }
      >
        <Empty description={error} />
      </Card>
    );
  }

  if (events.length === 0) {
    return (
      <Card
        title="Status Timeline"
        extra={
          <Button icon={<ReloadOutlined />} onClick={fetchEvents} size="small">
            Refresh
          </Button>
        }
      >
        <Empty description="No status events recorded" />
      </Card>
    );
  }

  return (
    <Card
      title="Status Timeline"
      extra={
        <Space>
          {loading && <Spin size="small" />}
          <Button icon={<ReloadOutlined />} onClick={fetchEvents} size="small">
            Refresh
          </Button>
        </Space>
      }
    >
      <Timeline
        mode="left"
        items={events.map((event) => ({
          color: getTimelineItemColor(event),
          label: (
            <Tooltip title={formatTime(event.createdAt)}>
              <Text type="secondary" style={{ fontSize: '12px' }}>
                {formatRelativeTime(event.createdAt)}
              </Text>
            </Tooltip>
          ),
          children: renderEventContent(event),
        }))}
      />
    </Card>
  );
};

export default StatusTimeline;
