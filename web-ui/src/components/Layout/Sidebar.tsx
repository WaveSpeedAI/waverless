import { Layout, Menu } from 'antd';
import {
  DashboardOutlined,
  RocketOutlined,
  AppstoreOutlined,
  DatabaseOutlined,
  UnorderedListOutlined,
  ThunderboltOutlined,
  CloudServerOutlined,
} from '@ant-design/icons';
import { useNavigate, useLocation } from 'react-router-dom';

const { Sider } = Layout;

interface SidebarProps {
  collapsed: boolean;
  onCollapse: (collapsed: boolean) => void;
}

const Sidebar = ({ collapsed, onCollapse }: SidebarProps) => {
  const navigate = useNavigate();
  const location = useLocation();

  const menuItems = [
    {
      key: '/dashboard',
      icon: <DashboardOutlined />,
      label: 'Dashboard',
    },
    {
      key: '/deploy',
      icon: <RocketOutlined />,
      label: 'Deploy App',
    },
    {
      key: '/apps',
      icon: <AppstoreOutlined />,
      label: 'Manage Apps',
    },
    {
      key: '/tasks',
      icon: <UnorderedListOutlined />,
      label: 'Task Queue',
    },
    {
      key: '/gpu-usage',
      icon: <CloudServerOutlined />,
      label: 'GPU Usage',
    },
    {
      key: '/specs',
      icon: <DatabaseOutlined />,
      label: 'Hardware Specs',
    },
  ];

  return (
    <Sider
      collapsible
      collapsed={collapsed}
      onCollapse={onCollapse}
      theme="dark"
      breakpoint="lg"
      style={{
        overflow: 'auto',
        height: '100vh',
        position: 'sticky',
        left: 0,
        top: 0,
        bottom: 0,
        boxShadow: '2px 0 8px rgba(0, 0, 0, 0.15)',
      }}
    >
      <div
        style={{
          height: 64,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          color: '#fff',
          fontSize: collapsed ? 18 : 22,
          fontWeight: 'bold',
          background: 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)',
          transition: 'all 0.2s',
        }}
      >
        {collapsed ? (
          <ThunderboltOutlined style={{ fontSize: 24 }} />
        ) : (
          'Waverless'
        )}
      </div>
      <Menu
        theme="dark"
        mode="inline"
        selectedKeys={[location.pathname]}
        items={menuItems}
        onClick={({ key }) => navigate(key)}
        style={{ borderRight: 0 }}
      />
    </Sider>
  );
};

export default Sidebar;
