import { Layout, Typography, Button, Space, Tag } from 'antd';
import { LogoutOutlined, ThunderboltOutlined } from '@ant-design/icons';
import { useNavigate, useLocation } from 'react-router-dom';
import { logout } from '@/utils/auth';

const { Header: AntHeader } = Layout;
const { Title, Text } = Typography;

interface HeaderProps {
  onToggleCollapse: () => void;
}

const Header = ({ onToggleCollapse }: HeaderProps) => {
  const navigate = useNavigate();
  const location = useLocation();

  const handleLogout = () => {
    logout();
    navigate('/login');
  };

  // Get page title based on current route
  const getPageTitle = () => {
    const titles: Record<string, string> = {
      '/dashboard': 'Dashboard',
      '/deploy': 'Deploy Application',
      '/apps': 'Manage Applications',
      '/tasks': 'Task Queue',
      '/specs': 'Hardware Specifications',
    };
    return titles[location.pathname] || 'Serverless Workload Platform';
  };

  return (
    <AntHeader
      style={{
        background: '#fff',
        padding: '16px 24px',
        height: 'auto',
        lineHeight: 'normal',
        boxShadow: '0 2px 8px rgba(0,0,0,0.06)',
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
        borderBottom: '1px solid #f0f0f0',
      }}
    >
      <Space size="middle" align="center">
        <ThunderboltOutlined
          style={{
            fontSize: 24,
            color: '#667eea',
            cursor: 'pointer',
          }}
          onClick={onToggleCollapse}
        />
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          <Title level={4} style={{ margin: 0, lineHeight: 1.2, color: '#262626' }}>
            {getPageTitle()}
          </Title>
          <Text type="secondary" style={{ fontSize: 12, lineHeight: 1.2 }}>
            Serverless Workload Platform
          </Text>
        </div>
      </Space>
      <Space size="middle">
        <Tag color="processing" icon={<ThunderboltOutlined />}>
          Auto-Scaling
        </Tag>
        <Button type="text" icon={<LogoutOutlined />} onClick={handleLogout}>
          Logout
        </Button>
      </Space>
    </AntHeader>
  );
};

export default Header;
