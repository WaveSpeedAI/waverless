import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  Card,
  Row,
  Col,
  Typography,
  Tag,
  Descriptions,
  Divider,
  Empty,
  Spin,
  Button,
  Space,
} from 'antd';
import { DatabaseOutlined, ReloadOutlined } from '@ant-design/icons';
import { api } from '@/api/client';
import type { SpecInfo } from '@/types';

const { Title, Paragraph, Text } = Typography;

const SpecsPage = () => {
  const [selectedCategory, setSelectedCategory] = useState<string>('all');

  // Fetch specs
  const { data: specs, isLoading, refetch } = useQuery({
    queryKey: ['specs'],
    queryFn: async () => {
      const response = await api.specs.list();
      return response.data;
    },
  });

  // Filter specs by category
  const filteredSpecs = specs?.filter(
    (spec) => selectedCategory === 'all' || spec.category === selectedCategory
  );

  // Get unique categories
  const categories = ['all', ...new Set(specs?.map((s) => s.category) || [])];

  const renderSpecCard = (spec: SpecInfo) => (
    <Card
      key={spec.name}
      title={
        <Space>
          <DatabaseOutlined />
          <span>{spec.displayName}</span>
        </Space>
      }
      extra={<Tag color={spec.category === 'gpu' ? 'blue' : 'green'}>{spec.category.toUpperCase()}</Tag>}
      hoverable
      style={{ marginBottom: 16 }}
    >
      <Descriptions column={1} size="small">
        <Descriptions.Item label="Name">
          <Text code>{spec.name}</Text>
        </Descriptions.Item>

        {spec.resources.gpu && (
          <Descriptions.Item label="GPU">
            <Tag color="blue">
              {spec.resources.gpu} × {spec.resources.gpuType}
            </Tag>
          </Descriptions.Item>
        )}

        <Descriptions.Item label="CPU">
          <Tag>{spec.resources.cpu} cores</Tag>
        </Descriptions.Item>

        <Descriptions.Item label="Memory">
          <Tag>{spec.resources.memory}</Tag>
        </Descriptions.Item>

        {spec.resources.disk && (
          <Descriptions.Item label="Disk">
            <Tag>{spec.resources.disk}GB</Tag>
          </Descriptions.Item>
        )}

        {spec.resources.ephemeralStorage && (
          <Descriptions.Item label="Ephemeral Storage">
            <Tag>{spec.resources.ephemeralStorage}GB</Tag>
          </Descriptions.Item>
        )}
      </Descriptions>

      {/* Show platform configs */}
      {Object.keys(spec.platforms).length > 0 && (
        <>
          <Divider style={{ margin: '12px 0' }} />
          <Text type="secondary" style={{ fontSize: 12 }}>
            Supported Platforms: {Object.keys(spec.platforms).join(', ')}
          </Text>
        </>
      )}
    </Card>
  );

  return (
    <div>
      <Row justify="space-between" align="middle" style={{ marginBottom: 12 }}>
        <Col>
          <Title level={3} style={{ margin: 0, marginBottom: 4 }}>
            <DatabaseOutlined /> GPU Specs
          </Title>
          <Paragraph type="secondary" style={{ margin: 0, fontSize: 13 }}>
            View available GPU and CPU specifications for deployment
          </Paragraph>
        </Col>
        <Col>
          <Button icon={<ReloadOutlined />} onClick={() => refetch()} loading={isLoading}>
            Refresh
          </Button>
        </Col>
      </Row>

      {/* Category Filter */}
      <Card style={{ marginBottom: 16 }}>
        <Space wrap>
          <Text strong>Filter by category:</Text>
          {categories.map((cat) => (
            <Tag.CheckableTag
              key={cat}
              checked={selectedCategory === cat}
              onChange={() => setSelectedCategory(cat)}
              style={{ padding: '4px 12px', fontSize: 14 }}
            >
              {cat.toUpperCase()}
            </Tag.CheckableTag>
          ))}
        </Space>
      </Card>

      <Spin spinning={isLoading}>
        {filteredSpecs && filteredSpecs.length > 0 ? (
          <Row gutter={[16, 16]}>
            {filteredSpecs.map((spec) => (
              <Col xs={24} sm={12} lg={8} key={spec.name}>
                {renderSpecCard(spec)}
              </Col>
            ))}
          </Row>
        ) : (
          <Card>
            <Empty description="No specs found" />
          </Card>
        )}
      </Spin>
    </div>
  );
};

export default SpecsPage;
