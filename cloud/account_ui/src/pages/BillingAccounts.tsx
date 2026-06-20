import { useState } from 'react';
import { useAuth } from '../auth/AuthProvider';
import { useNavItems } from './useNavItems';
import {
  PageShell, AppDataGrid, AppDialog, ConfirmDialog, AppTextField,
  type GridColDef,
} from '../components';
import {
  useBillingAccounts, useCreateBillingAccount, useUpdateBillingAccount, useDeleteBillingAccount,
} from '../datastore';
import type { BillingAccount } from '../api/services/BillingAccountsService';
import {
  Box, Button, IconButton, Typography, Tooltip,
  AddIcon, EditIcon, DeleteIcon, LogoutIcon,
} from '../components';

const BASE_COLUMNS: GridColDef<BillingAccount>[] = [
  { field: 'id', headerName: 'ID', flex: 1.5 },
  { field: 'name', headerName: 'Name', flex: 1 },
  { field: 'principal_account_id', headerName: 'Principal Account', flex: 1.5 },
  { field: 'stripe_customer_id', headerName: 'Stripe Customer', flex: 1 },
  { field: 'created_at', headerName: 'Created', flex: 1, valueFormatter: (v: string) => v ? new Date(v).toLocaleString() : '' },
];

export function BillingAccounts() {
  const { user, logout } = useAuth();
  const navItems = useNavItems();

  const { data, isPending, isError } = useBillingAccounts();
  const createMutation = useCreateBillingAccount();
  const updateMutation = useUpdateBillingAccount();
  const deleteMutation = useDeleteBillingAccount();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<BillingAccount | null>(null);
  const [formName, setFormName] = useState('');
  const [formPrincipalId, setFormPrincipalId] = useState('');
  const [dialogError, setDialogError] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<BillingAccount | null>(null);

  const openCreate = () => {
    setEditing(null);
    setFormName('');
    setFormPrincipalId(user?.principal_account_id ?? '');
    setDialogError(null);
    setDialogOpen(true);
  };

  const openEdit = (row: BillingAccount) => {
    setEditing(row);
    setFormName(row.name);
    setFormPrincipalId(row.principal_account_id);
    setDialogError(null);
    setDialogOpen(true);
  };

  const handleSubmit = async () => {
    setDialogError(null);
    try {
      if (editing) {
        await updateMutation.mutateAsync({ id: editing.id, req: { name: formName } });
      } else {
        await createMutation.mutateAsync({ name: formName, principal_account_id: formPrincipalId });
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

  const columns: GridColDef<BillingAccount>[] = [
    ...BASE_COLUMNS,
    {
      field: '__actions',
      headerName: '',
      width: 80,
      sortable: false,
      renderCell: ({ row }) => (
        <Box sx={{ display: 'flex', gap: 0.5 }}>
          <Tooltip title="Edit">
            <IconButton size="small" onClick={() => openEdit(row)}>
              <EditIcon fontSize="small" />
            </IconButton>
          </Tooltip>
          <Tooltip title="Delete">
            <IconButton size="small" onClick={() => setDeleteTarget(row)}>
              <DeleteIcon fontSize="small" />
            </IconButton>
          </Tooltip>
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
            <IconButton size="small" color="inherit" onClick={logout}>
              <LogoutIcon fontSize="small" />
            </IconButton>
          </Tooltip>
        </Box>
      }
    >
      <Box sx={{ p: 2, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Typography variant="h6">Billing Accounts</Typography>
        <Button variant="contained" size="small" startIcon={<AddIcon />} onClick={openCreate}>New</Button>
      </Box>
      <Box sx={{ flex: 1, px: 2, pb: 2 }}>
        <AppDataGrid
          rows={rows}
          columns={columns}
          loading={isPending}
          emptyMessage={isError ? 'Failed to load billing accounts.' : 'No billing accounts yet.'}
          sx={{ flex: 1 }}
        />
      </Box>
      <AppDialog
        open={dialogOpen}
        title={editing ? 'Edit Billing Account' : 'New Billing Account'}
        loading={createMutation.isPending || updateMutation.isPending}
        error={dialogError}
        onClose={() => setDialogOpen(false)}
        onSubmit={handleSubmit}
      >
        <AppTextField
          label="Name"
          value={formName}
          onChange={(e) => setFormName(e.target.value)}
          fullWidth
          required
          autoFocus
        />
        {!editing && (
          <AppTextField
            label="Principal Account ID"
            value={formPrincipalId}
            onChange={(e) => setFormPrincipalId(e.target.value)}
            fullWidth
            required
            helperText="Defaults to your principal account"
          />
        )}
      </AppDialog>
      <ConfirmDialog
        open={!!deleteTarget}
        title="Delete Billing Account"
        message={`Delete "${deleteTarget?.name}"? This cannot be undone.`}
        loading={deleteMutation.isPending}
        onClose={() => setDeleteTarget(null)}
        onConfirm={handleDelete}
      />
    </PageShell>
  );
}
