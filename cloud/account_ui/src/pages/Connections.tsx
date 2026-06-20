import { useState } from 'react';
import { useAuth } from '../auth/AuthProvider';
import { useNavItems } from './useNavItems';
import {
  PageShell, AppDataGrid, AppDialog, ConfirmDialog, AppTextField,
  type GridColDef,
} from '../components';
import { useConnections, useCreateConnection, useUpdateConnection, useDeleteConnection } from '../datastore';
import type { Connection } from '../api/services/ConnectionsService';
import {
  Box, Button, IconButton, Typography, Tooltip,
  AddIcon, EditIcon, DeleteIcon, LogoutIcon,
} from '../components';

// -- Constants -----------------------------------------------------------------

const BASE_COLUMNS: GridColDef<Connection>[] = [
  { field: 'id', headerName: 'ID', flex: 1.5 },
  { field: 'name', headerName: 'Name', flex: 1 },
  { field: 'tunnel_id', headerName: 'Tunnel ID', flex: 1.5 },
  { field: 'created_at', headerName: 'Created', flex: 1, valueFormatter: (v: string) => v ? new Date(v).toLocaleString() : '' },
];

// -- Component -----------------------------------------------------------------

export function Connections() {
  const { user, logout } = useAuth();
  const navItems = useNavItems();

  const { data, isPending, isError } = useConnections();
  const createMutation = useCreateConnection();
  const updateMutation = useUpdateConnection();
  const deleteMutation = useDeleteConnection();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<Connection | null>(null);
  const [formName, setFormName] = useState('');
  const [formTunnelId, setFormTunnelId] = useState('');
  const [dialogError, setDialogError] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Connection | null>(null);

  const openCreate = () => {
    setEditing(null);
    setFormName('');
    setFormTunnelId('');
    setDialogError(null);
    setDialogOpen(true);
  };

  const openEdit = (row: Connection) => {
    setEditing(row);
    setFormName(row.name);
    setFormTunnelId(row.tunnel_id);
    setDialogError(null);
    setDialogOpen(true);
  };

  const handleSubmit = async () => {
    setDialogError(null);
    try {
      if (editing) {
        await updateMutation.mutateAsync({ id: editing.id, req: { name: formName } });
      } else {
        await createMutation.mutateAsync({ name: formName, tunnel_id: formTunnelId });
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

  const columns: GridColDef<Connection>[] = [
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
        <Typography variant="h6">Connections</Typography>
        <Button variant="contained" size="small" startIcon={<AddIcon />} onClick={openCreate}>New</Button>
      </Box>
      <Box sx={{ flex: 1, px: 2, pb: 2 }}>
        <AppDataGrid
          rows={rows}
          columns={columns}
          loading={isPending}
          emptyMessage={isError ? 'Failed to load connections.' : 'No connections yet.'}
          sx={{ flex: 1 }}
        />
      </Box>

      <AppDialog
        open={dialogOpen}
        title={editing ? 'Edit Connection' : 'New Connection'}
        loading={createMutation.isPending || updateMutation.isPending}
        error={dialogError}
        onClose={() => setDialogOpen(false)}
        onSubmit={handleSubmit}
      >
        <AppTextField label="Name" value={formName} onChange={(e) => setFormName(e.target.value)} fullWidth required autoFocus />
        {!editing && (
          <AppTextField label="Tunnel ID" value={formTunnelId} onChange={(e) => setFormTunnelId(e.target.value)} fullWidth required />
        )}
      </AppDialog>

      <ConfirmDialog
        open={!!deleteTarget}
        title="Delete Connection"
        message={`Delete connection "${deleteTarget?.name}"? This cannot be undone.`}
        loading={deleteMutation.isPending}
        onClose={() => setDeleteTarget(null)}
        onConfirm={handleDelete}
      />
    </PageShell>
  );
}
