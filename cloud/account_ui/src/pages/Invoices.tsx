import { useAuth } from '../auth/AuthProvider';
import { useNavItems } from './useNavItems';
import {
  PageShell, AppDataGrid, Box, Typography, Chip, Link,
  IconButton, Tooltip, LogoutIcon,
  type GridColDef,
} from '../components';
import { useCurrentBillingAccount, useInvoices } from '../datastore';
import { formatCents } from '../format';
import type { Invoice } from '../api/services/InvoicesService';

const STATUS_COLOR: Record<string, 'success' | 'warning' | 'error' | 'default'> = {
  paid: 'success',
  open: 'warning',
  uncollectible: 'error',
  void: 'default',
  draft: 'default',
};

const COLUMNS: GridColDef<Invoice>[] = [
  { field: 'stripe_invoice_id', headerName: 'Invoice', flex: 1.2 },
  {
    field: 'status',
    headerName: 'Status',
    flex: 0.8,
    renderCell: ({ value }) => (
      <Chip label={value as string} size="small" color={STATUS_COLOR[value as string] ?? 'default'} />
    ),
  },
  {
    field: 'amount_due_cents',
    headerName: 'Amount Due',
    flex: 0.8,
    valueGetter: (_v, row) => formatCents(row.amount_due_cents, row.currency),
  },
  {
    field: 'amount_paid_cents',
    headerName: 'Amount Paid',
    flex: 0.8,
    valueGetter: (_v, row) => formatCents(row.amount_paid_cents, row.currency),
  },
  { field: 'created_at', headerName: 'Date', flex: 1, valueFormatter: (v: string) => (v ? new Date(v).toLocaleString() : '') },
  {
    field: 'hosted_invoice_url',
    headerName: '',
    flex: 0.6,
    sortable: false,
    renderCell: ({ value }) => (value ? <Link href={value as string} target="_blank" rel="noreferrer">View</Link> : null),
  },
];

export function Invoices() {
  const { user, logout } = useAuth();
  const navItems = useNavItems();

  const { billingAccount, isPending: billingAccountsPending } = useCurrentBillingAccount();

  const { data, isPending, isError } = useInvoices(billingAccount?.id);
  const rows = data?.items ?? [];

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
      <Box sx={{ p: 2 }}>
        <Typography variant="h6">Invoices</Typography>
      </Box>
      <Box sx={{ flex: 1, px: 2, pb: 2 }}>
        <AppDataGrid
          rows={rows}
          columns={COLUMNS}
          loading={isPending || billingAccountsPending}
          emptyMessage={isError ? 'Failed to load invoices.' : 'No invoices yet.'}
          sx={{ flex: 1 }}
        />
      </Box>
    </PageShell>
  );
}
