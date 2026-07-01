import { Routes, Route, Navigate } from 'react-router-dom';
import { ProtectedRoute } from './auth/ProtectedRoute';
import { Login, Signup, BillingAccounts, Clusters, ClusterRegistration, Streams, Connections, Subscriptions } from './pages';

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/signup" element={<Signup />} />
      <Route path="/billing-accounts" element={<ProtectedRoute><BillingAccounts /></ProtectedRoute>} />
      <Route path="/clusters" element={<ProtectedRoute><Clusters /></ProtectedRoute>} />
      <Route path="/clusters/register" element={<ProtectedRoute><ClusterRegistration /></ProtectedRoute>} />
      <Route path="/streams" element={<ProtectedRoute><Streams /></ProtectedRoute>} />
      <Route path="/connections" element={<ProtectedRoute><Connections /></ProtectedRoute>} />
      <Route path="/subscriptions" element={<ProtectedRoute><Subscriptions /></ProtectedRoute>} />
      <Route path="*" element={<Navigate to="/billing-accounts" replace />} />
    </Routes>
  );
}
