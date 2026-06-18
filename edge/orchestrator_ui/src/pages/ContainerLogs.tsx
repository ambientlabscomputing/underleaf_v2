import { useMemo, useRef, useEffect } from 'react';
import { useNavigate, useParams, useLocation } from 'react-router-dom';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import CircularProgress from '@mui/material/CircularProgress';
import IconButton from '@mui/material/IconButton';
import Typography from '@mui/material/Typography';
import ArrowBackIcon from '@mui/icons-material/ArrowBack';
import HubIcon from '@mui/icons-material/Hub';
import StorageIcon from '@mui/icons-material/Storage';

import { PageShell, type NavItem } from '../components';
import { useContainerLogHistory, useLiveContainerLogs } from '../datastore';
import type { Container } from '../api/services/ContainersService';

// ── Helpers ───────────────────────────────────────────────────────────────────

function formatTs(tsMs: number): string {
  return new Date(tsMs).toISOString().replace('T', ' ').replace('Z', '');
}

// Pull history from one hour ago by default.
const DEFAULT_SINCE_MS = () => Date.now() - 60 * 60 * 1000;

// ── Component ─────────────────────────────────────────────────────────────────

export function ContainerLogs() {
  const navigate = useNavigate();
  const { dockerId = '' } = useParams<{ dockerId: string }>();
  const { state } = useLocation() as { state: { container?: Container } | null };
  const container = state?.container;

  const sinceMs = useMemo(() => DEFAULT_SINCE_MS(), []);
  const logEndRef = useRef<HTMLDivElement>(null);

  const { data: historyPage, isPending } = useContainerLogHistory(dockerId, sinceMs);
  const { lines: liveLines, connected } = useLiveContainerLogs(dockerId, sinceMs);

  const historyLines = historyPage?.lines ?? [];

  // Auto-scroll to bottom when new lines arrive.
  useEffect(() => {
    logEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [liveLines.length, historyLines.length]);

  const navItems: NavItem[] = [
    {
      label: 'Nodes',
      icon: <HubIcon fontSize="small" />,
      onClick: () => navigate('/nodes'),
      selected: false,
    },
    {
      label: 'Containers',
      icon: <StorageIcon fontSize="small" />,
      onClick: () => navigate('/containers'),
      selected: false,
    },
  ];

  return (
    <PageShell
      title="Orchestrator"
      navItems={navItems}
      actions={
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Chip
            size="small"
            label={connected ? 'Live' : 'Disconnected'}
            color={connected ? 'success' : 'default'}
            variant="outlined"
          />
        </Box>
      }
    >
      {/* Header row */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, px: 2, py: 1, borderBottom: 1, borderColor: 'divider' }}>
        <IconButton size="small" onClick={() => navigate('/containers')} aria-label="back to containers">
          <ArrowBackIcon fontSize="small" />
        </IconButton>
        <Typography variant="subtitle2" sx={{ fontFamily: 'monospace' }}>
          {container?.image ?? dockerId.substring(0, 12)}
        </Typography>
        <Typography variant="caption" color="text.secondary" sx={{ fontFamily: 'monospace' }}>
          {dockerId.substring(0, 12)}
        </Typography>
      </Box>

      {/* Log output */}
      <Box
        sx={{
          flex: 1,
          overflow: 'auto',
          bgcolor: '#0d1117',
          px: 2,
          py: 1,
          fontFamily: 'monospace',
          fontSize: '0.8rem',
        }}
      >
        {isPending && (
          <Box sx={{ display: 'flex', justifyContent: 'center', pt: 4 }}>
            <CircularProgress size={24} />
          </Box>
        )}

        {historyLines.map((line, i) => (
          <Box key={`h-${i}`} sx={{ display: 'flex', gap: 1.5, lineHeight: 1.6 }}>
            <Typography
              component="span"
              sx={{ color: '#8b949e', fontSize: 'inherit', fontFamily: 'inherit', whiteSpace: 'nowrap', flexShrink: 0 }}
            >
              {formatTs(line.ts_ms)}
            </Typography>
            <Typography
              component="span"
              sx={{
                color: line.stream === 'stderr' ? '#f85149' : '#e6edf3',
                fontSize: 'inherit',
                fontFamily: 'inherit',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-all',
              }}
            >
              {line.message}
            </Typography>
          </Box>
        ))}

        {liveLines.map((line, i) => (
          <Box key={`l-${i}`} sx={{ display: 'flex', gap: 1.5, lineHeight: 1.6 }}>
            <Typography
              component="span"
              sx={{ color: '#8b949e', fontSize: 'inherit', fontFamily: 'inherit', whiteSpace: 'nowrap', flexShrink: 0 }}
            >
              {formatTs(line.ts_ms)}
            </Typography>
            <Typography
              component="span"
              sx={{
                color: line.stream === 'stderr' ? '#f85149' : '#e6edf3',
                fontSize: 'inherit',
                fontFamily: 'inherit',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-all',
              }}
            >
              {line.message}
            </Typography>
          </Box>
        ))}

        {!isPending && historyLines.length === 0 && liveLines.length === 0 && (
          <Typography sx={{ color: '#8b949e', fontSize: 'inherit', fontFamily: 'inherit', pt: 2 }}>
            No logs in the last hour. Waiting for output…
          </Typography>
        )}

        <div ref={logEndRef} />
      </Box>
    </PageShell>
  );
}
