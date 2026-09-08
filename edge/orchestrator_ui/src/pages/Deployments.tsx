import { useNavigate } from 'react-router-dom';

import {
  AppDataGrid,
  DeployForm,
  HubIcon,
  PageShell,
  RocketLaunchIcon,
  StorageIcon,
  type DeployFormFeedback,
  type GridColDef,
  type NavItem,
} from '../components';
import { useCreateDeployment, useDeployments } from '../datastore';
import type { Deployment } from '../api/services/DeploymentsService';

// ── Column definitions ────────────────────────────────────────────────────────

const columns: GridColDef<Deployment>[] = [
  {
    field: 'id',
    headerName: 'ID',
    width: 220,
    sortable: false,
  },
  {
    field: 'name',
    headerName: 'Name',
    flex: 1,
    minWidth: 160,
    sortable: false,
    valueGetter: (_value, row) => row.spec?.name ?? '',
  },
  {
    field: 'repo',
    headerName: 'Repo',
    flex: 1,
    minWidth: 220,
  },
  {
    field: 'ref',
    headerName: 'Ref',
    width: 140,
  },
  {
    field: 'services',
    headerName: 'Services',
    width: 100,
    sortable: false,
    valueGetter: (_value, row) => row.spec?.services?.length ?? 0,
  },
  {
    field: 'status',
    headerName: 'Status',
    width: 120,
  },
];

// ── Page ──────────────────────────────────────────────────────────────────────

export function Deployments() {
  const navigate = useNavigate();
  const { data: deployments = [], isPending, isError } = useDeployments();
  const createDeployment = useCreateDeployment();

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
    {
      label: 'Deployments',
      icon: <RocketLaunchIcon fontSize="small" />,
      onClick: () => {},
      selected: true,
    },
  ];

  // Reconcile now runs in the background — a successful POST only means the
  // manifest resolved and was persisted (status starts "in_progress"). This
  // feedback is for request-level failures only (bad source, validation);
  // reconcile outcomes show up live in the Status column instead.
  let feedback: DeployFormFeedback | undefined;
  if (createDeployment.isError) {
    feedback = { severity: 'error', message: (createDeployment.error as Error).message };
  }

  return (
    <PageShell title="Orchestrator" navItems={navItems}>
      <DeployForm
        onSubmit={(values) => createDeployment.mutate(values)}
        isPending={createDeployment.isPending}
        feedback={feedback}
      />
      <AppDataGrid
        rows={deployments}
        columns={columns}
        loading={isPending}
        emptyMessage={isError ? 'Failed to load deployments.' : 'No deployments yet — deploy something above.'}
        sx={{ flex: 1 }}
      />
    </PageShell>
  );
}
