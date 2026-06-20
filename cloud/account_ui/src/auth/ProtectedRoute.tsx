import { Navigate } from 'react-router-dom';
import { useAuth } from './AuthProvider';

export function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth();

  if (loading) return null; // AuthProvider resolves quickly; no spinner needed
  if (!user) return <Navigate to="/login" replace />;
  return <>{children}</>;
}
