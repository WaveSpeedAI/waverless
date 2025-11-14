import { useState } from 'react';
import { Form, Input, Button, Card, message, Typography, Space } from 'antd';
import { UserOutlined, LockOutlined, ThunderboltOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';

const { Title, Text } = Typography;

const LoginPage = () => {
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();

  const onFinish = (values: { username: string; password: string }) => {
    setLoading(true);

    // Get credentials from environment variables (injected at build time)
    const adminUsername = import.meta.env.VITE_ADMIN_USERNAME || 'admin';
    const adminPassword = import.meta.env.VITE_ADMIN_PASSWORD || 'admin';

    if (values.username === adminUsername && values.password === adminPassword) {
      // Store auth token in localStorage
      localStorage.setItem('auth_token', 'authenticated');
      message.success('Login successful!');
      navigate('/');
    } else {
      message.error('Invalid username or password');
    }

    setLoading(false);
  };

  return (
    <div
      style={{
        display: 'flex',
        justifyContent: 'center',
        alignItems: 'center',
        height: '100vh',
        background: 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)',
      }}
    >
      <Card
        style={{
          width: 420,
          boxShadow: '0 8px 24px rgba(0,0,0,0.12)',
          borderRadius: 12,
        }}
      >
        <Space direction="vertical" size="large" style={{ width: '100%' }}>
          <div style={{ textAlign: 'center' }}>
            <ThunderboltOutlined
              style={{
                fontSize: 48,
                color: '#667eea',
                marginBottom: 16,
              }}
            />
            <Title level={2} style={{ margin: '0 0 8px 0' }}>
              Waverless
            </Title>
            <Text type="secondary">Serverless Workload Platform</Text>
          </div>
          <Form
            name="login"
            initialValues={{ remember: true }}
            onFinish={onFinish}
            autoComplete="off"
            layout="vertical"
          >
            <Form.Item
              name="username"
              rules={[{ required: true, message: 'Please input your username!' }]}
            >
              <Input
                prefix={<UserOutlined />}
                placeholder="Username"
                size="large"
              />
            </Form.Item>

            <Form.Item
              name="password"
              rules={[{ required: true, message: 'Please input your password!' }]}
            >
              <Input.Password
                prefix={<LockOutlined />}
                placeholder="Password"
                size="large"
              />
            </Form.Item>

            <Form.Item>
              <Button
                type="primary"
                htmlType="submit"
                loading={loading}
                block
                size="large"
                style={{
                  background: 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)',
                  border: 'none',
                }}
              >
                Log in
              </Button>
            </Form.Item>
          </Form>
        </Space>
      </Card>
    </div>
  );
};

export default LoginPage;
