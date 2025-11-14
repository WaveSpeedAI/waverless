import { useState } from 'react';
import { Routes, Route, Navigate, useLocation } from 'react-router-dom';
import { Layout } from 'antd';
import Sidebar from './components/Layout/Sidebar';
import Header from './components/Layout/Header';
import DashboardPage from './pages/Dashboard';
import DeployPage from './pages/Deploy';
import AppsPage from './pages/Apps';
import SpecsPage from './pages/Specs';
import TasksPage from './pages/Tasks';
import GPUUsagePage from './pages/GPUUsage';
import LoginPage from './pages/Login';
import { isAuthenticated } from './utils/auth';

const { Content } = Layout;

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const location = useLocation();

  if (!isAuthenticated()) {
    return <Navigate to="/login" state={{ from: location }} replace />;
  }

  return <>{children}</>;
}

function App() {
  const location = useLocation();
  const isLoginPage = location.pathname === '/login';
  const [collapsed, setCollapsed] = useState(false);

  if (isLoginPage) {
    return (
      <Routes>
        <Route path="/login" element={<LoginPage />} />
      </Routes>
    );
  }

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sidebar collapsed={collapsed} onCollapse={setCollapsed} />
      <Layout>
        <Header onToggleCollapse={() => setCollapsed(!collapsed)} />
        <Content
          style={{
            margin: '24px 16px',
            padding: 24,
            background: '#f0f2f5',
            minHeight: 280,
            borderRadius: 8,
          }}
        >
          <Routes>
            <Route path="/" element={<Navigate to="/dashboard" replace />} />
            <Route path="/dashboard" element={<ProtectedRoute><DashboardPage /></ProtectedRoute>} />
            <Route path="/deploy" element={<ProtectedRoute><DeployPage /></ProtectedRoute>} />
            <Route path="/apps" element={<ProtectedRoute><AppsPage /></ProtectedRoute>} />
            <Route path="/specs" element={<ProtectedRoute><SpecsPage /></ProtectedRoute>} />
            <Route path="/tasks" element={<ProtectedRoute><TasksPage /></ProtectedRoute>} />
            <Route path="/gpu-usage" element={<ProtectedRoute><GPUUsagePage /></ProtectedRoute>} />
          </Routes>
        </Content>
      </Layout>
    </Layout>
  );
}

export default App;
