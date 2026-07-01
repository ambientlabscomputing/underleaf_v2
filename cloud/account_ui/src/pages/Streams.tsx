import { useState } from 'react';
import { useAuth } from '../auth/AuthProvider';
import { useNavItems } from './useNavItems';
import {
  PageShell, AppDataGrid, AppDialog, ConfirmDialog, AppTextField,
  type GridColDef,
} from '../components';
import { useStreams, useCreateStream, useUpdateStream, useDeleteStream } from '../datastore';
import type { Stream } from '../api/services/StreamsService';
import {
  Box, Button, IconButton, Typography, Tooltip,
  AddIcon, EditIcon, DeleteIcon, LogoutIcon,
} from '../components';

// -- Constants -----------------------------------------------------------------

const BASE_COLUMNS: GridColDef<Stream>[] = [
  { field: 'id', headerName: 'ID', flex: 1.5 },
  { field: 'name', headerName: 'Name', flex: 1 },
  { field: 'node_id', headerName: 'Node ID', flex: 1.5 },
  { field: 'created_at', headerName: 'Created', flex: 1, valueFormatter: (v: string) => v ? new Date(v).toLocaleString() : '' },
];

// -- Component -----------------------------------------------------------------

export function Streams() {
  const { user, logout } = useAuth();
  const navItems = useNavItems();

  const { data, isPending, isError } = useStreams();
  const createMutation = useCreateStream();
  const updateMutation = useUpdateStream();
  const deleteMutation = useDeleteStream();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<Stream | null>(null);
  const [formName, setFormName] = useState('');
  const [formNodeId, setFormNodeId] = useState('');
  const [dialogError, setDialogError] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Stream | null>(null);

  const openCreate = () => {
    setEditing(null);
    setFormName('');
    setFormNodeId('');
    setDialogError(null);
    setDialogOpen(true);
  };

  const openEdit = (row: Stream) => {
    setEditing(row);
    setFormName(row.name);
    setFormNodeId(row.node_id);
    setDialogError(null);
    setDialogOpen(true);
  };

  const handleSubmit = async () => {
    setDialogError(null);
    try {
      if (editing) {
        await updateMutation.mutateAsync({ id: editing.id, req: { name: formName } });
      } else {
        await createMutation.mutateAsync({ name: formName, node_id: formNodeId });
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

  const columns: GridColDef<Stream>[] = [
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
        <Typography variant="h6">Streams</Typography>
        <Button variant="contained" size="small" startIcon={<AddIcon />} onClick={openCreate}>New</Button>
      </Box>
      <Box sx={{ flex: 1, px: 2, pb: 2 }}>
        <AppDataGrid
          rows={rows}
          columns={columns}
          loading={isPending}
          emptyMessage={isError ? 'Failed to load streams.' : 'No streams yet.'}
          sx={{ flex: 1 }}
        />
      </Box>

      <AppDialog
        open={dialogOpen}
        title={editing ? 'Edit Stream' : 'New Stream'}
        loading={createMutation.isPending || updateMutation.isPending}
        error={dialogError}
        onClose={() => setDialogOpen(false)}
        onSubmit={handleSubmit}
      >
        <AppTextField label="Name" value={formName} onChange={(e) => setFormName(e.target.value)} fullWidth required autoFocus />
        {!editing && (
          <AppTextField label="Node ID" value={formNodeId} onChange={(e) => setFormNodeId(e.target.value)} fullWidth required />
        )}
      </AppDialog>

      <ConfirmDialog
        open={!!deleteTarget}
        title="Delete Stream"
        message={`Delete stream "${deleteTarget?.name}"? This cannot be undone.`}
        loading={deleteMutation.isPending}
        onClose={() => setDeleteTarget(null)}
        onConfirm={handleDelete}
      />
    </PageShell>
  );
}
