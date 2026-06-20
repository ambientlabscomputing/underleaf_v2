import { useState } from 'react';
import { useAuth } from '../auth/AuthProvider';
import { useNavItems } from './useNavItems';
import {
  PageShell, AppDataGrid, AppDialog, ConfirmDialog, AppTextField, AppMenuItem,
  type GridColDef,
} from '../components';
import { useSubscriptions, useCreateSubscription, useUpdateSubscription, useDeleteSubscription } from '../datastore';
import type { Subscription, SubscriptionTier } from '../api/services/SubscriptionsService';
import {
  Box, Button, IconButton, Typography, Tooltip, Chip, Select, InputLabel, FormControl,
  AddIcon, EditIcon, DeleteIcon, LogoutIcon,
} from '../components';
import type { SelectChangeEvent } from '../components';

// -- Constants -----------------------------------------------------------------

const TIER_COLOR: Record<SubscriptionTier, 'default' | 'primary' | 'secondary'> = {
  free: 'default',
  builder: 'primary',
  pro: 'secondary',
};

const BASE_COLUMNS: GridColDef<Subscription>[] = [
  { field: 'id', headerName: 'ID', flex: 1.5 },
  {
    field: 'tier',
    headerName: 'Tier',
    width: 100,
    renderCell: ({ value }) => <Chip label={value} color={TIER_COLOR[value as SubscriptionTier]} size="small" />,
  },
  { field: 'billing_account_id', headerName: 'Billing Account', flex: 1.5 },
  { field: 'created_at', headerName: 'Created', flex: 1, valueFormatter: (v: string) => v ? new Date(v).toLocaleString() : '' },
];

// -- Component -----------------------------------------------------------------

export function Subscriptions() {
  const { user, logout } = useAuth();
  const navItems = useNavItems();

  const { data, isPending, isError } = useSubscriptions();
  const createMutation = useCreateSubscription();
  const updateMutation = useUpdateSubscription();
  const deleteMutation = useDeleteSubscription();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<Subscription | null>(null);
  const [formBillingId, setFormBillingId] = useState('');
  const [formTier, setFormTier] = useState<SubscriptionTier>('free');
  const [dialogError, setDialogError] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Subscription | null>(null);

  const openCreate = () => {
    setEditing(null);
    setFormBillingId('');
    setFormTier('free');
    setDialogError(null);
    setDialogOpen(true);
  };

  const openEdit = (row: Subscription) => {
    setEditing(row);
    setFormBillingId(row.billing_account_id);
    setFormTier(row.tier);
    setDialogError(null);
    setDialogOpen(true);
  };

  const handleSubmit = async () => {
    setDialogError(null);
    try {
      if (editing) {
        await updateMutation.mutateAsync({ id: editing.id, req: { tier: formTier } });
      } else {
        await createMutation.mutateAsync({ billing_account_id: formBillingId, tier: formTier });
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

  const columns: GridColDef<Subscription>[] = [
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
        <Typography variant="h6">Subscriptions</Typography>
        <Button variant="contained" size="small" startIcon={<AddIcon />} onClick={openCreate}>New</Button>
      </Box>
      <Box sx={{ flex: 1, px: 2, pb: 2 }}>
        <AppDataGrid
          rows={rows}
          columns={columns}
          loading={isPending}
          emptyMessage={isError ? 'Failed to load subscriptions.' : 'No subscriptions yet.'}
          sx={{ flex: 1 }}
        />
      </Box>

      <AppDialog
        open={dialogOpen}
        title={editing ? 'Edit Subscription' : 'New Subscription'}
        loading={createMutation.isPending || updateMutation.isPending}
        error={dialogError}
        onClose={() => setDialogOpen(false)}
        onSubmit={handleSubmit}
      >
        {!editing && (
          <AppTextField label="Billing Account ID" value={formBillingId} onChange={(e) => setFormBillingId(e.target.value)} fullWidth required autoFocus />
        )}
        <FormControl fullWidth>
          <InputLabel>Tier</InputLabel>
          <Select
            label="Tier"
            value={formTier}
            onChange={(e: SelectChangeEvent) => setFormTier(e.target.value as SubscriptionTier)}
          >
            <AppMenuItem value="free">Free</AppMenuItem>
            <AppMenuItem value="builder">Builder</AppMenuItem>
            <AppMenuItem value="pro">Pro</AppMenuItem>
          </Select>
        </FormControl>
      </AppDialog>

      <ConfirmDialog
        open={!!deleteTarget}
        title="Delete Subscription"
        message={`Delete subscription "${deleteTarget?.id}"? This cannot be undone.`}
        loading={deleteMutation.isPending}
        onClose={() => setDeleteTarget(null)}
        onConfirm={handleDelete}
      />
    </PageShell>
  );
}
