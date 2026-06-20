import { useState } from 'react';
import { useAuth } from '../auth/AuthProvider';
import { useNavItems } from './useNavItems';
import {
  PageShell, AppDataGrid, AppDialog, ConfirmDialog, AppTextField,
  type GridColDef,
} from '../components';
import {
  useClusters, useCreateCluster, useUpdateCluster, useDeleteCluster,
} from '../datastore';
import type { Cluster } from '../api/services/ClustersService';
import {
  Box, Button, IconButton, Typography, Tooltip,
  AddIcon, EditIcon, DeleteIcon, LogoutIcon,
} from '../components';

// -- Constants -----------------------------------------------------------------

const BASE_COLUMNS: GridColDef<Cluster>[] = [
  { field: 'id', headerName: 'ID', flex: 1 },
  { field: 'name', headerName: 'Name', flex: 1 },
  { field: 'principal_account_id', headerName: 'Principal Account', flex: 1.5 },
  { field: 'created_at', headerName: 'Created', flex: 1, valueFormatter: (v: string) => v ? new Date(v).toLocaleString() : '' },
];

// -- Component -----------------------------------------------------------------

export function Clusters() {
  const { user, logout } = useAuth();
  const navItems = useNavItems();

  const { data, isPending, isError } = useClusters();
  const createMutation = useCreateCluster();
  const updateMutation = useUpdateCluster();
  const deleteMutation = useDeleteCluster();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<Cluster | null>(null);
  const [formId, setFormId] = useState('');
  const [formName, setFormName] = useState('');
  const [dialogError, setDialogError] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Cluster | null>(null);

  const openCreate = () => {
    setEditing(null);
    setFormId('');
    setFormName('');
    setDialogError(null);
    setDialogOpen(true);
  };

  const openEdit = (row: Cluster) => {
    setEditing(row);
    setFormId(row.id);
    setFormName(row.name);
    setDialogError(null);
    setDialogOpen(true);
  };

  const handleSubmit = async () => {
    setDialogError(null);
    try {
      if (editing) {
        await updateMutation.mutateAsync({ id: editing.id, req: { name: formName } });
      } else {
        await createMutation.mutateAsync({ id: formId, name: formName });
      }
      setDialogOpen(false);
    } catch (err) {
      setDialogError(err instanceof Error ? err.message : 'Error');
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try {
      await deleteMutation.mutateAsync(deleteTarget.id);
      setDeleteTarget(null);
    } catch { /* leave dialog open */ }
  };

  const rows = data?.items ?? [];

  const columns: GridColDef<Cluster>[] = [
    ...BASE_COLUMNS,
    {
      field: '__actions',
      headerName: '',
      width: 80,
      sortable: false,
      renderCell: ({ row }) => (
        <Box sx={{ display: 'flex', gap: 0.5 }}>
          <Tooltip title="Edit"><IconButton size="small" onClick={() => openEdit(row)}><EditIcon fontSize="small" /></IconButton></Tooltip>
          <Tooltip title="Delete"><IconButton size="small" onClick={() => setDeleteTarget(row)}><DeleteIcon fontSize="small" /></IconButton></Tooltip>
        </Box>
      ),
    },
  ];

  return (
    <PageShell
      title="Cloud Admin"
      navItems={navItems}
      actions={
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Typography variant="caption" color="text.secondary">{user?.email}</Typography>
          <Tooltip title="Sign out" placement="bottom">
            <IconButton size="small" color="inherit" onClick={logout}><LogoutIcon fontSize="small" /></IconButton>
          </Tooltip>
        </Box>
      }
    >
      <Box sx={{ p: 2, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Typography variant="h6">Clusters</Typography>
        <Button variant="contained" size="small" startIcon={<AddIcon />} onClick={openCreate}>New</Button>
      </Box>
      <Box sx={{ flex: 1, px: 2, pb: 2 }}>
        <AppDataGrid
          rows={rows}
          columns={columns}
          loading={isPending}
          emptyMessage={isError ? 'Failed to load clusters.' : 'No clusters yet.'}
          sx={{ flex: 1 }}
        />
      </Box>

      <AppDialog
        open={dialogOpen}
        title={editing ? 'Edit Cluster' : 'New Cluster'}
        loading={createMutation.isPending || updateMutation.isPending}
        error={dialogError}
        onClose={() => setDialogOpen(false)}
        onSubmit={handleSubmit}
      >
        {!editing && (
          <AppTextField label="ID" value={formId} onChange={(e) => setFormId(e.target.value)} fullWidth required autoFocus helperText="Unique cluster identifier" />
        )}
        <AppTextField label="Name" value={formName} onChange={(e) => setFormName(e.target.value)} fullWidth required autoFocus={!!editing} />
      </AppDialog>

      <ConfirmDialog
        open={!!deleteTarget}
        title="Delete Cluster"
        message={`Delete cluster "${deleteTarget?.name}"? This cannot be undone.`}
        loading={deleteMutation.isPending}
        onClose={() => setDeleteTarget(null)}
        onConfirm={handleDelete}
      />
    </PageShell>
  );
}
