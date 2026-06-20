import { useSearchParams, useNavigate } from 'react-router-dom';
import {
  PageShell,
  Box,
  Button,
  Typography,
  Alert,
  CircularProgress,
  Paper,
} from '../components';
import { useCandidate, useApproveRegistration } from '../datastore';

export function ClusterRegistration() {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const userCode = searchParams.get('user_code');

  const { data: candidate, isLoading, error } = useCandidate(userCode);
  const approve = useApproveRegistration();

  const handleConfirm = async () => {
    if (!userCode) return;
    try {
      await approve.mutateAsync(userCode);
      navigate(`/clusters`);
    } catch {
      // error shown via approve.error below
    }
  };

  return (
    <PageShell title="Cloud Admin">
      <Box sx={{ p: 4, maxWidth: 480 }}>
        <Typography variant="h5" sx={{ fontWeight: 700, mb: 3 }}>
          Register Cluster
        </Typography>

        {!userCode && (
          <Alert severity="error">Missing registration code. Please use the URL provided by <code>orcli cloud register</code>.</Alert>
        )}

        {userCode && isLoading && <CircularProgress size={24} />}

        {error && (
          <Alert severity="error" sx={{ mb: 2 }}>
            Failed to load registration details. The link may have expired.
          </Alert>
        )}

        {candidate && candidate.status === 'pending' && (
          <Paper variant="outlined" sx={{ p: 3 }}>
            <Typography variant="body1" sx={{ mb: 1 }}>
              A cluster is requesting to be linked to your account.
            </Typography>
            <Box sx={{ my: 2 }}>
              <Typography variant="body2" color="text.secondary">Cluster name</Typography>
              <Typography variant="body1" sx={{ fontWeight: 600 }}>{candidate.proposed_cluster_name}</Typography>
            </Box>
            <Box sx={{ mb: 2 }}>
              <Typography variant="body2" color="text.secondary">Cluster ID</Typography>
              <Typography variant="body1" sx={{ fontFamily: 'monospace', fontSize: '0.85rem' }}>{candidate.proposed_cluster_id}</Typography>
            </Box>

            {approve.error && (
              <Alert severity="error" sx={{ mb: 2 }}>
                {approve.error instanceof Error ? approve.error.message : 'Approval failed'}
              </Alert>
            )}

            <Box sx={{ display: 'flex', gap: 2, mt: 3 }}>
              <Button
                variant="contained"
                onClick={handleConfirm}
                disabled={approve.isPending}
                startIcon={approve.isPending ? <CircularProgress size={14} /> : undefined}
              >
                Confirm
              </Button>
              <Button variant="outlined" onClick={() => navigate('/clusters')}>
                Cancel
              </Button>
            </Box>
          </Paper>
        )}

        {candidate && candidate.status !== 'pending' && (
          <Alert severity="warning">
            This registration is no longer active (status: {candidate.status}).
          </Alert>
        )}
      </Box>
    </PageShell>
  );
}
