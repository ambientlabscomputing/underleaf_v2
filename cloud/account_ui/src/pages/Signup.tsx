import { useState } from 'react';
import { useNavigate, Link as RouterLink } from 'react-router-dom';
import { authService } from '../api/services/AuthService';
import { useAuth } from '../auth/AuthProvider';
import { AppTextField, Box, Button, Paper, Typography, Alert, CircularProgress, Link } from '../components';

// -- Component -----------------------------------------------------------------

export function Signup() {
  const { login } = useAuth();
  const navigate = useNavigate();
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setLoading(true);
    try {
      await authService.signup({ name, email, password });
      await login(email, password);
      navigate('/billing-accounts', { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Sign-up failed');
    } finally {
      setLoading(false);
    }
  };

  return (
    <Box sx={{ minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', bgcolor: 'background.default' }}>
      <Paper sx={{ p: 4, width: '100%', maxWidth: 400 }} variant="outlined">
        <Typography variant="h5" sx={{ fontWeight: 700, mb: 3 }}>Create account</Typography>
        {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}
        <Box component="form" onSubmit={handleSubmit} sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <AppTextField label="Name" value={name} onChange={(e) => setName(e.target.value)} required fullWidth autoFocus />
          <AppTextField label="Email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required fullWidth />
          <AppTextField label="Password" type="password" value={password} onChange={(e) => setPassword(e.target.value)} required fullWidth />
          <Button type="submit" variant="contained" fullWidth disabled={loading}
            startIcon={loading ? <CircularProgress size={14} /> : undefined}>
            Create account
          </Button>
        </Box>
        <Typography variant="body2" sx={{ mt: 2 }} color="text.secondary">
          Already have an account?{' '}
          <Link component={RouterLink} to="/login" underline="hover">Sign in</Link>
        </Typography>
      </Paper>
    </Box>
  );
}
