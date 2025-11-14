import { useState } from 'react';
import { useQuery, useMutation } from '@tanstack/react-query';
import { Card, Row, Col, Statistic, DatePicker, Select, Space, Button, Spin, Alert, Tabs, message, Modal, Typography } from 'antd';
import { Line, Column } from '@ant-design/plots';
import { ReloadOutlined, SyncOutlined, DashboardOutlined } from '@ant-design/icons';
import dayjs, { Dayjs } from 'dayjs';
import { api } from '@/api/client';

const { Title, Paragraph } = Typography;

const { RangePicker } = DatePicker;
const { Option } = Select;
const { TabPane } = Tabs;

type ScopeType = 'global' | 'endpoint' | 'spec';
type Granularity = 'minute' | 'hourly' | 'daily';

export default function GPUUsage() {
  const [granularity, setGranularity] = useState<Granularity>('hourly');
  const [scopeType, setScopeType] = useState<ScopeType>('global');
  const [scopeValue, setScopeValue] = useState<string>('');
  const [dateRange, setDateRange] = useState<[Dayjs, Dayjs]>([
    dayjs().subtract(24, 'hours'),
    dayjs(),
  ]);

  const params = {
    scope_type: scopeType,
    scope_value: scopeValue || undefined,
    start_time: dateRange[0].toISOString(),
    end_time: dateRange[1].toISOString(),
  };

  const { data: minuteData, refetch: refetchMinute, isLoading: isLoadingMinute } = useQuery({
    queryKey: ['gpu-usage-minute', params],
    queryFn: async () => {
      const response = await api.gpuUsage.getMinuteStats(params);
      return response.data;
    },
    enabled: granularity === 'minute',
  });

  const { data: hourlyData, refetch: refetchHourly, isLoading: isLoadingHourly } = useQuery({
    queryKey: ['gpu-usage-hourly', params],
    queryFn: async () => {
      const response = await api.gpuUsage.getHourlyStats(params);
      return response.data;
    },
    enabled: granularity === 'hourly',
  });

  const { data: dailyData, refetch: refetchDaily, isLoading: isLoadingDaily } = useQuery({
    queryKey: ['gpu-usage-daily', params],
    queryFn: async () => {
      const response = await api.gpuUsage.getDailyStats(params);
      return response.data;
    },
    enabled: granularity === 'daily',
  });

  const currentData = granularity === 'minute' ? minuteData : granularity === 'hourly' ? hourlyData : dailyData;
  const isLoading = granularity === 'minute' ? isLoadingMinute : granularity === 'hourly' ? isLoadingHourly : isLoadingDaily;

  const handleRefresh = () => {
    if (granularity === 'minute') refetchMinute();
    else if (granularity === 'hourly') refetchHourly();
    else refetchDaily();
  };

  const handleDateRangeChange = (dates: [Dayjs | null, Dayjs | null] | null) => {
    if (dates && dates[0] && dates[1]) {
      setDateRange([dates[0], dates[1]]);
    }
  };

  // Backfill historical data mutation
  const backfillMutation = useMutation({
    mutationFn: async (params?: { batch_size?: number; max_tasks?: number }) => {
      const response = await api.gpuUsage.backfillHistoricalData(params);
      return response.data;
    },
    onSuccess: (data) => {
      message.success(
        `Sync completed! Processed ${data.totalTasksProcessed} tasks, created ${data.recordsCreated} records, took ${data.duration}`
      );
      // Show detailed modal
      Modal.info({
        title: 'Historical Data Sync Result',
        width: 600,
        content: (
          <div>
            <p><strong>Total Tasks:</strong> {data.totalTasksProcessed}</p>
            <p><strong>Records Created:</strong> {data.recordsCreated}</p>
            <p><strong>Tasks Skipped:</strong> {data.recordsSkipped}</p>
            <p><strong>Duration:</strong> {data.duration}</p>
            {data.errors && data.errors.length > 0 && (
              <>
                <p><strong>Errors ({data.errors.length}):</strong></p>
                <ul style={{ maxHeight: '200px', overflow: 'auto' }}>
                  {data.errors.slice(0, 10).map((err, idx) => (
                    <li key={idx}>{err}</li>
                  ))}
                  {data.errors.length > 10 && <li>... and {data.errors.length - 10} more errors</li>}
                </ul>
              </>
            )}
            <p style={{ marginTop: '16px', color: '#666' }}>
              Note: After sync completes, the system will automatically aggregate statistics in the background. You can also click the "Refresh" button to refresh data.
            </p>
          </div>
        ),
      });
      // Refresh data
      handleRefresh();
    },
    onError: (error: any) => {
      message.error(`Sync failed: ${error.message || 'Unknown error'}`);
    },
  });

  const handleBackfill = () => {
    Modal.confirm({
      title: 'Confirm Historical GPU Usage Data Sync?',
      content: (
        <div>
          <p>This operation will create GPU usage records for all completed tasks that don't have GPU usage records yet.</p>
          <p><strong>Note:</strong></p>
          <ul>
            <li>Uses current spec.yaml configuration (may not be fully accurate if historical spec has changed)</li>
            <li>This process may take several minutes depending on the number of historical tasks</li>
            <li>Tasks with existing records will be automatically skipped</li>
          </ul>
        </div>
      ),
      onOk: () => {
        backfillMutation.mutate({ batch_size: 1000, max_tasks: 0 });
      },
    });
  };

  // Aggregate statistics mutation
  const aggregateMutation = useMutation({
    mutationFn: async (params?: { granularity?: string; start_time?: string; end_time?: string }) => {
      const response = await api.gpuUsage.triggerAggregation(params);
      return response.data;
    },
    onSuccess: (data) => {
      message.success(`Aggregation completed! Granularity: ${data.granularity}`);
      handleRefresh();
    },
    onError: (error: any) => {
      message.error(`Aggregation failed: ${error.message || 'Unknown error'}`);
    },
  });

  const handleAggregate = () => {
    Modal.confirm({
      title: 'Confirm GPU Statistics Aggregation?',
      content: (
        <div>
          <p>This operation will aggregate statistics (minute/hourly/daily) from GPU usage records.</p>
          <p><strong>Description:</strong></p>
          <ul>
            <li>This operation needs to be executed after syncing historical data to generate statistical reports</li>
            <li>Will aggregate all data within the currently selected time range</li>
            <li>Existing statistical data will be updated</li>
          </ul>
        </div>
      ),
      onOk: () => {
        aggregateMutation.mutate({
          granularity: 'all',
          start_time: dateRange[0].toISOString(),
          end_time: dateRange[1].toISOString(),
        });
      },
    });
  };

  // Prepare chart data
  const gpuHoursChartData = currentData?.data.map((item: any) => ({
    time: dayjs(item.time_bucket).format(granularity === 'daily' ? 'MM-DD' : granularity === 'hourly' ? 'MM-DD HH:mm' : 'HH:mm'),
    value: item.total_gpu_hours,
    type: 'GPU Hours',
  })) || [];

  const tasksChartData = currentData?.data.flatMap((item: any) => [
    {
      time: dayjs(item.time_bucket).format(granularity === 'daily' ? 'MM-DD' : granularity === 'hourly' ? 'MM-DD HH:mm' : 'HH:mm'),
      value: item.completed_tasks,
      type: 'Completed',
    },
    {
      time: dayjs(item.time_bucket).format(granularity === 'daily' ? 'MM-DD' : granularity === 'hourly' ? 'MM-DD HH:mm' : 'HH:mm'),
      value: item.failed_tasks,
      type: 'Failed',
    },
  ]) || [];

  const avgGpuCountChartData = currentData?.data.map((item: any) => ({
    time: dayjs(item.time_bucket).format(granularity === 'daily' ? 'MM-DD' : granularity === 'hourly' ? 'MM-DD HH:mm' : 'HH:mm'),
    value: item.avg_gpu_count,
    type: 'Average GPUs',
  })) || [];

  return (
    <div>
      <Row justify="space-between" align="middle" style={{ marginBottom: 12 }}>
        <Col>
          <Title level={3} style={{ margin: 0, marginBottom: 4 }}>
            <DashboardOutlined /> GPU Usage Statistics
          </Title>
          <Paragraph type="secondary" style={{ margin: 0, fontSize: 13 }}>
            Monitor GPU resource consumption and task metrics
          </Paragraph>
        </Col>
      </Row>

      {/* Filters */}
      <Card style={{ marginBottom: 24 }}>
        <Space size="middle" wrap>
          <span>Granularity:</span>
          <Select value={granularity} onChange={setGranularity} style={{ width: 120 }}>
            <Option value="minute">Minute</Option>
            <Option value="hourly">Hourly</Option>
            <Option value="daily">Daily</Option>
          </Select>

          <span>Scope:</span>
          <Select value={scopeType} onChange={(val) => { setScopeType(val); setScopeValue(''); }} style={{ width: 120 }}>
            <Option value="global">Global</Option>
            <Option value="endpoint">Endpoint</Option>
            <Option value="spec">Spec</Option>
          </Select>

          {scopeType !== 'global' && (
            <input
              type="text"
              placeholder={`Enter ${scopeType} name`}
              value={scopeValue}
              onChange={(e) => setScopeValue(e.target.value)}
              style={{ padding: '4px 11px', borderRadius: '6px', border: '1px solid #d9d9d9' }}
            />
          )}

          <span>Time Range:</span>
          <RangePicker
            showTime={granularity !== 'daily'}
            value={dateRange}
            onChange={handleDateRangeChange}
            format={granularity === 'daily' ? 'YYYY-MM-DD' : 'YYYY-MM-DD HH:mm'}
          />

          <Button icon={<ReloadOutlined />} onClick={handleRefresh}>
            Refresh
          </Button>

          <Button
            type="primary"
            icon={<SyncOutlined />}
            onClick={handleBackfill}
            loading={backfillMutation.isPending}
          >
            Sync Historical Data
          </Button>

          <Button
            type="default"
            onClick={handleAggregate}
            loading={aggregateMutation.isPending}
          >
            Aggregate Statistics
          </Button>
        </Space>
      </Card>

      {/* Summary Statistics */}
      {isLoading ? (
        <div style={{ textAlign: 'center', padding: '50px' }}>
          <Spin size="large" />
        </div>
      ) : currentData ? (
        <>
          <Row gutter={16} style={{ marginBottom: 24 }}>
            <Col span={6}>
              <Card>
                <Statistic
                  title="Total GPU Hours"
                  value={currentData.summary.total_gpu_hours.toFixed(2)}
                  precision={2}
                />
              </Card>
            </Col>
            <Col span={6}>
              <Card>
                <Statistic title="Total Tasks" value={currentData.summary.total_tasks} />
              </Card>
            </Col>
            {granularity === 'hourly' && 'max_gpu_count' in currentData.summary && (
              <Col span={6}>
                <Card>
                  <Statistic title="Max GPU Count" value={currentData.summary.max_gpu_count} />
                </Card>
              </Col>
            )}
            {granularity === 'daily' && 'avg_gpu_hours_per_day' in currentData.summary && (
              <Col span={6}>
                <Card>
                  <Statistic
                    title="Avg GPU Hours/Day"
                    value={currentData.summary.avg_gpu_hours_per_day.toFixed(2)}
                    precision={2}
                  />
                </Card>
              </Col>
            )}
            <Col span={6}>
              <Card>
                <Statistic title="Data Points" value={currentData.total} />
              </Card>
            </Col>
          </Row>

          {/* Charts */}
          <Tabs defaultActiveKey="gpu-hours">
            <TabPane tab="GPU Hours Over Time" key="gpu-hours">
              <Card>
                <Line
                  data={gpuHoursChartData}
                  xField="time"
                  yField="value"
                  seriesField="type"
                  smooth
                  height={400}
                  xAxis={{
                    label: {
                      autoRotate: true,
                      autoHide: true,
                    },
                  }}
                  yAxis={{
                    title: {
                      text: 'GPU Card-Hours',
                    },
                  }}
                  tooltip={{
                    formatter: (datum: any) => ({
                      name: datum.type,
                      value: `${datum.value.toFixed(2)} hours`,
                    }),
                  }}
                />
              </Card>
            </TabPane>

            <TabPane tab="Tasks Distribution" key="tasks">
              <Card>
                <Column
                  data={tasksChartData}
                  xField="time"
                  yField="value"
                  seriesField="type"
                  isStack
                  height={400}
                  xAxis={{
                    label: {
                      autoRotate: true,
                      autoHide: true,
                    },
                  }}
                  yAxis={{
                    title: {
                      text: 'Number of Tasks',
                    },
                  }}
                  color={['#52c41a', '#ff4d4f']}
                />
              </Card>
            </TabPane>

            <TabPane tab="Average GPU Count" key="avg-gpu">
              <Card>
                <Line
                  data={avgGpuCountChartData}
                  xField="time"
                  yField="value"
                  seriesField="type"
                  smooth
                  height={400}
                  xAxis={{
                    label: {
                      autoRotate: true,
                      autoHide: true,
                    },
                  }}
                  yAxis={{
                    title: {
                      text: 'Average GPU Count',
                    },
                  }}
                  tooltip={{
                    formatter: (datum: any) => ({
                      name: datum.type,
                      value: `${datum.value.toFixed(2)} GPUs`,
                    }),
                  }}
                />
              </Card>
            </TabPane>
          </Tabs>
        </>
      ) : (
        <Alert message="No data available for the selected time range and filters." type="info" showIcon />
      )}
    </div>
  );
}
