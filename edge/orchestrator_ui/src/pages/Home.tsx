import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import HubIcon from '@mui/icons-material/Hub';
import StorageIcon from '@mui/icons-material/Storage';

import { AppDataGrid, PageShell, type GridColDef, type GridSortModel, type NavItem } from '../components';
import { useNodes } from '../datastore';
import type { Node } from '../api/services/NodesService';
import type { GetNodesRequest } from '../api/services/NodesService';

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
  {
    field: 'ip_address',
    headerName: 'IP Address',
    width: 160,
  },
  {
    field: 'os',
    headerName: 'OS',
    width: 120,
  },
  {
    field: 'arch',
    headerName: 'Arch',
    width: 100,
  },
];

// ── Page ──────────────────────────────────────────────────────────────────────

export function Home() {
  const navigate = useNavigate();
  const [query, setQuery] = useState<GetNodesRequest>({});
  const { data: nodes = [], isPending, isError } = useNodes(query);

  const handleSortModelChange = (model: GridSortModel) => {
    const [sort] = model;
    setQuery(prev => ({
      ...prev,
      orderBy: sort?.field,
      order: sort?.sort ?? undefined,
    }));
  };

  const navItems: NavItem[] = [
    {
      label: 'Nodes',
      icon: <HubIcon fontSize="small" />,
      onClick: () => {},
      selected: true,
    },
    {
      label: 'Containers',
      icon: <StorageIcon fontSize="small" />,
      onClick: () => navigate('/containers'),
      selected: false,
    },
  ];

  return (
    <PageShell title="Orchestrator" navItems={navItems}>
      <AppDataGrid
        rows={nodes}
        columns={columns}
        loading={isPending}
        emptyMessage={isError ? 'Failed to load nodes.' : 'No nodes registered yet.'}
        sortingMode="server"
        onSortModelChange={handleSortModelChange}
        sx={{ flex: 1 }}
      />
    </PageShell>
  );
}
