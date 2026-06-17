import HubIcon from '@mui/icons-material/Hub';
import StorageIcon from '@mui/icons-material/Storage';

import { AppDataGrid, PageShell, type GridColDef, type NavItem } from '../components';
import { useContainers } from '../datastore';
import type { Container } from '../api/services/ContainersService';

// ── Column definitions ────────────────────────────────────────────────────────

const columns: GridColDef<Container>[] = [
  {
    field: 'docker_id',
    headerName: 'Docker ID',
    width: 180,
    sortable: false,
    valueFormatter: (value: string) => value?.substring(0, 12) || '',
  },
  {
    field: 'image',
    headerName: 'Image',
    flex: 1,
    minWidth: 200,
  },
  {
    field: 'status',
    headerName: 'Status',
    width: 120,
  },
  {
    field: 'node_id',
    headerName: 'Node',
    width: 180,
    sortable: false,
    valueFormatter: (value: string) => value?.substring(0, 12) || '',
  },
  {
    field: 'uptime',
    headerName: 'Created',
    width: 180,
    sortable: false,
    valueFormatter: (value: number) => new Date(value * 1000).toLocaleString(),
  },
];

// ── Nav ───────────────────────────────────────────────────────────────────────

export interface ContainersProps {
  onNavigate?: (page: 'nodes' | 'containers') => void;
}

const buildNavItems = (onNavigate: ContainersProps['onNavigate']): NavItem[] => [
  {
    label: 'Nodes',
    icon: <HubIcon fontSize="small" />,
    onClick: () => onNavigate?.('nodes'),
    selected: false,
  },
  {
    label: 'Containers',
    icon: <StorageIcon fontSize="small" />,
    onClick: () => {},
    selected: true,
  },
];

// ── Page ──────────────────────────────────────────────────────────────────────

export function Containers({ onNavigate }: ContainersProps) {
  const { data: containers = [], isPending, isError } = useContainers();

  return (
    <PageShell title="Orchestrator" navItems={buildNavItems(onNavigate)}>
      <AppDataGrid
        rows={containers}
        columns={columns}
        loading={isPending}
        emptyMessage={isError ? 'Failed to load containers.' : 'No containers synced yet. Check agent status.'}
        sx={{ flex: 1 }}
      />
    </PageShell>
  );
}
