import HubIcon from '@mui/icons-material/Hub';
import { AppDataGrid, PageShell, type GridColDef, type NavItem } from '../components';
import { useNodes } from '../datastore';
import type { Node } from '../api/services/NodesService';

// ── Column definitions ────────────────────────────────────────────────────────

const columns: GridColDef<Node>[] = [
  {
    field: 'id',
    headerName: 'ID',
    width: 240,
    sortable: false,
  },
  {
    field: 'name',
    headerName: 'Name',
    flex: 1,
    minWidth: 160,
  },
];

// ── Nav ───────────────────────────────────────────────────────────────────────

const navItems: NavItem[] = [
  {
    label: 'Nodes',
    icon: <HubIcon fontSize="small" />,
    onClick: () => {},
    selected: true,
  },
];

// ── Page ──────────────────────────────────────────────────────────────────────

export function Home() {
  const { data: nodes = [], isPending, isError } = useNodes();

  return (
    <PageShell title="Orchestrator" navItems={navItems}>
      <AppDataGrid
        rows={nodes}
        columns={columns}
        loading={isPending}
        emptyMessage={isError ? 'Failed to load nodes.' : 'No nodes registered yet.'}
        sx={{ flex: 1 }}
      />
    </PageShell>
  );
}
