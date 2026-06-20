import { useState } from 'react';
import { useNavigate, Link as RouterLink } from 'react-router-dom';
import { useAuth } from '../auth/AuthProvider';
import { AppTextField, Box, Button, Paper, Typography, Alert, CircularProgress, Link } from '../components';

// -- Component -----------------------------------------------------------------

export function Login() {
  const { login } = useAuth();
  const navigate = useNavigate();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setLoading(true);
    try {
      await login(email, password);
      navigate('/billing-accounts', { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed');
    } finally {
      setLoading(false);
    }
  };

  return (
    <Box sx={{ minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', bgcolor: 'background.default' }}>
      <Paper sx={{ p: 4, width: '100%', maxWidth: 400 }} variant="outlined">
        <Typography variant="h5" sx={{ fontWeight: 700, mb: 3 }}>Sign in</Typography>
        {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}
        <Box component="form" onSubmit={handleSubmit} sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <AppTextField label="Email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required fullWidth autoFocus />
          <AppTextField label="Password" type="password" value={password} onChange={(e) => setPassword(e.target.value)} required fullWidth />
          <Button type="submit" variant="contained" fullWidth disabled={loading}
            startIcon={loading ? <CircularProgress size={14} /> : undefined}>
            Sign in
          </Button>
        </Box>
        <Typography variant="body2" sx={{ mt: 2 }} color="text.secondary">
          No account?{' '}
          <Link component={RouterLink} to="/signup" underline="hover">Sign up</Link>
        </Typography>
      </Paper>
    </Box>
  );
}
