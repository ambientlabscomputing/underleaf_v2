import { useState } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import {
  PageShell,
  Box,
  Button,
  Typography,
  Alert,
  CircularProgress,
  Paper,
  AppTextField,
} from '../components';
import { useDeviceLoginRequest, useApproveDeviceLogin, useDenyDeviceLogin } from '../datastore';

// The page `ufctl login` points at (verification_uri). The signed-in owner sees
// the device name and the code ufctl printed, and approves or denies. Approving
// hands that machine a revocable device key for this account.
export function UfctlActivate() {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const userCode = searchParams.get('user_code');
  const [typedCode, setTypedCode] = useState('');

  const { data: request, isLoading, error } = useDeviceLoginRequest(userCode);
  const approve = useApproveDeviceLogin();
  const deny = useDenyDeviceLogin();
  const [decision, setDecision] = useState<'approved' | 'denied' | null>(null);

  const busy = approve.isPending || deny.isPending;
  const actionError = approve.error ?? deny.error;

  const handleApprove = async () => {
    if (!userCode) return;
    try {
      await approve.mutateAsync(userCode);
      setDecision('approved');
    } catch {
      // shown via actionError
    }
  };

  const handleDeny = async () => {
    if (!userCode) return;
    try {
      await deny.mutateAsync(userCode);
      setDecision('denied');
    } catch {
      // shown via actionError
    }
  };

  return (
    <PageShell title="Cloud Admin">
      <Box sx={{ p: 4, maxWidth: 480 }}>
        <Typography variant="h5" sx={{ fontWeight: 700, mb: 3 }}>
          Sign in to ufctl
        </Typography>

        {!userCode && (
          <Paper variant="outlined" sx={{ p: 3 }}>
            <Typography variant="body1" sx={{ mb: 2 }}>
              Enter the code shown by <code>ufctl login</code>.
            </Typography>
            <Box
              component="form"
              onSubmit={(e: React.FormEvent) => {
                e.preventDefault();
                if (typedCode.trim()) {
                  navigate(`/ufctl/activate?user_code=${encodeURIComponent(typedCode.trim())}`);
                }
              }}
              sx={{ display: 'flex', gap: 2 }}
            >
              <AppTextField
                label="Code"
                value={typedCode}
                onChange={(e) => setTypedCode(e.target.value)}
                placeholder="ABCD-EFGH"
                autoFocus
                fullWidth
              />
              <Button type="submit" variant="contained" disabled={!typedCode.trim()}>
                Continue
              </Button>
            </Box>
          </Paper>
        )}

        {userCode && isLoading && <CircularProgress size={24} />}

        {error && (
          <Alert severity="error" sx={{ mb: 2 }}>
            This login request was not found or has expired. Run <code>ufctl login</code> again.
          </Alert>
        )}

        {request && request.status === 'pending' && !decision && (
          <Paper variant="outlined" sx={{ p: 3 }}>
            <Typography variant="body1" sx={{ mb: 1 }}>
              A device is asking to sign in to your account and open your clusters.
            </Typography>
            <Box sx={{ my: 2 }}>
              <Typography variant="body2" color="text.secondary">Device</Typography>
              <Typography variant="body1" sx={{ fontWeight: 600 }}>{request.device_name}</Typography>
            </Box>
            <Box sx={{ mb: 2 }}>
              <Typography variant="body2" color="text.secondary">Code</Typography>
              <Typography variant="body1" sx={{ fontFamily: 'monospace', fontSize: '1.1rem' }}>
                {request.user_code}
              </Typography>
            </Box>
            <Alert severity="warning" sx={{ mb: 2 }}>
              Approve only if this is the code your terminal is showing right now. An approved
              device can reach every cluster in your account until you revoke it.
            </Alert>

            {actionError && (
              <Alert severity="error" sx={{ mb: 2 }}>
                {actionError instanceof Error ? actionError.message : 'Request failed'}
              </Alert>
            )}

            <Box sx={{ display: 'flex', gap: 2, mt: 3 }}>
              <Button
                variant="contained"
                onClick={handleApprove}
                disabled={busy}
                startIcon={approve.isPending ? <CircularProgress size={14} /> : undefined}
              >
                Approve
              </Button>
              <Button variant="outlined" color="error" onClick={handleDeny} disabled={busy}>
                Deny
              </Button>
            </Box>
          </Paper>
        )}

        {decision === 'approved' && (
          <Alert severity="success">
            Approved. Return to your terminal; <code>ufctl login</code> will finish on its own.
          </Alert>
        )}

        {decision === 'denied' && (
          <Alert severity="info">Denied. The device was not given access.</Alert>
        )}

        {request && request.status !== 'pending' && !decision && (
          <Alert severity="warning">
            This login request is no longer active (status: {request.status}).
          </Alert>
        )}
      </Box>
    </PageShell>
  );
}
