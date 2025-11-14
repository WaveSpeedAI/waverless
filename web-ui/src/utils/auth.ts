export const isAuthenticated = (): boolean => {
  return localStorage.getItem('auth_token') === 'authenticated';
};

export const logout = (): void => {
  localStorage.removeItem('auth_token');
};
