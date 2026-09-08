import { useState } from 'react';
import Alert from '@mui/material/Alert';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import CircularProgress from '@mui/material/CircularProgress';
import TextField from '@mui/material/TextField';

// ── Types ─────────────────────────────────────────────────────────────────────

export interface DeployFormValues {
  source: string;
  ref?: string;
  token?: string;
}

export interface DeployFormFeedback {
  severity: 'error' | 'warning';
  message: string;
}

export interface DeployFormProps {
  onSubmit: (values: DeployFormValues) => void;
  isPending?: boolean;
  feedback?: DeployFormFeedback;
}

// ── Component ─────────────────────────────────────────────────────────────────

export function DeployForm({ onSubmit, isPending, feedback }: DeployFormProps) {
  const [source, setSource] = useState('');
  const [ref, setRef] = useState('');
  const [token, setToken] = useState('');

  const handleSubmit = (event: React.FormEvent) => {
    event.preventDefault();
    const trimmed = source.trim();
    if (!trimmed) return;
    onSubmit({ source: trimmed, ref: ref.trim() || undefined, token: token.trim() || undefined });
  };

  return (
    <Box component="form" onSubmit={handleSubmit} sx={{ p: 2 }}>
      <Box sx={{ display: 'flex', gap: 1.5, alignItems: 'flex-start', flexWrap: 'wrap' }}>
        <TextField
          label="Source"
          placeholder="gh:owner/repo"
          size="small"
          value={source}
          onChange={(e) => setSource(e.target.value)}
          required
          sx={{ minWidth: 260 }}
        />
        <TextField
          label="Ref (optional)"
          placeholder="branch, tag, or SHA"
          size="small"
          value={ref}
          onChange={(e) => setRef(e.target.value)}
          sx={{ minWidth: 180 }}
        />
        <TextField
          label="Token (optional)"
          placeholder="for private repos"
          size="small"
          type="password"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          sx={{ minWidth: 180 }}
        />
        <Button type="submit" variant="contained" disabled={isPending} sx={{ height: 40 }}>
          {isPending ? <CircularProgress size={20} color="inherit" /> : 'Deploy'}
        </Button>
      </Box>
      {feedback && (
        <Alert severity={feedback.severity} sx={{ mt: 1.5 }}>
          {feedback.message}
        </Alert>
      )}
    </Box>
  );
}
