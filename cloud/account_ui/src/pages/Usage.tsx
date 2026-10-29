import { useState } from 'react';
import { useAuth } from '../auth/AuthProvider';
import { useNavItems } from './useNavItems';
import {
  PageShell, Box, Button, Typography, Alert, CircularProgress, Paper, LinearProgress,
  Switch, FormControlLabel, AppTextField, IconButton, Tooltip, LogoutIcon,
} from '../components';
import {
  useCurrentBillingAccount, useUsage, useUpdateBillingAccount, useBuyTopUp,
} from '../datastore';
import type { UsageSummary } from '../api/services/BillingAccountsService';
import { formatBytes, formatCents, formatDate } from '../format';

type Banner = { severity: 'info' | 'warning' | 'error'; text: string; offerAutoTopUp?: boolean };

/** What the overage policy says to tell the customer right now. */
function bannersFor(u: UsageSummary): Banner[] {
  const out: Banner[] = [];
  const resets = u.period_end ? ` Your allowance resets on ${formatDate(u.period_end)}.` : '';
  const kinds = new Set(u.alerts.map((a) => a.kind));

  if (u.blocked) {
    out.push({
      severity: 'error',
      text: u.paid
        ? 'Gateway traffic is stopped: your data and grace allowance are used up. Buy a top-up or upgrade to resume.'
        : `Gateway traffic is stopped: you've used your free allowance. Upgrade to resume.${resets}`,
      offerAutoTopUp: u.paid && !u.auto_top_up_enabled,
    });
  } else if (u.grace_active) {
    out.push({
      severity: 'warning',
      text: `You're out of included data and running on a temporary grace allowance (${formatBytes(u.grace_remaining_bytes)} left).`,
      offerAutoTopUp: !u.auto_top_up_enabled,
    });
  } else if (u.paid && u.percent_used >= 95) {
    out.push({
      severity: 'warning',
      text: `You've used ${u.percent_used.toFixed(0)}% of your data.`,
      offerAutoTopUp: !u.auto_top_up_enabled,
    });
  } else if (u.paid && u.percent_used >= 80) {
    out.push({ severity: 'info', text: `You've used ${u.percent_used.toFixed(0)}% of your data this period.` });
  }
  if (kinds.has('top_up_failed')) {
    out.push({ severity: 'error', text: 'A data top-up payment failed. Check your payment method.' });
  }
  if (kinds.has('top_up_cap_reached')) {
    out.push({
      severity: 'warning',
      text: `Your monthly auto top-up limit (${formatCents(u.monthly_top_up_cap_cents)}) was reached, so no more automatic purchases will be made this period.`,
    });
  }
  return out;
}

export function Usage() {
  const { user, logout } = useAuth();
  const navItems = useNavItems();

  const { billingAccount, isPending: accountPending } = useCurrentBillingAccount();
  const { data: usage, isPending: usagePending, isError } = useUsage(billingAccount?.id);
  const update = useUpdateBillingAccount();
  const buyTopUp = useBuyTopUp(billingAccount?.id);

  const [capDraft, setCapDraft] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const run = async (fn: () => Promise<unknown>) => {
    setActionError(null);
    try {
      await fn();
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Something went wrong');
    }
  };

  const setAutoTopUp = (enabled: boolean) =>
    run(() => update.mutateAsync({ id: billingAccount!.id, req: { auto_top_up_enabled: enabled } }));

  const saveCap = () =>
    run(async () => {
      const dollars = Number(capDraft);
      if (!Number.isFinite(dollars) || dollars < 0) throw new Error('Enter a valid dollar amount');
      await update.mutateAsync({
        id: billingAccount!.id,
        req: { monthly_top_up_cap_cents: Math.round(dollars * 100) },
      });
      setCapDraft(null);
    });

  const meterColor = !usage
    ? 'primary'
    : usage.blocked
      ? 'error'
      : usage.grace_active || usage.percent_used >= 95
        ? 'warning'
        : 'primary';

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
        <Typography variant="h6">Usage</Typography>
      </Box>

      <Box sx={{ px: 2, pb: 2, maxWidth: 720, display: 'flex', flexDirection: 'column', gap: 2 }}>
        {(accountPending || usagePending) && <CircularProgress size={28} />}
        {isError && <Alert severity="error">Failed to load usage.</Alert>}
        {actionError && <Alert severity="error">{actionError}</Alert>}

        {usage && (
          <>
            {bannersFor(usage).map((b) => (
              <Alert
                key={b.text}
                severity={b.severity}
                action={
                  b.offerAutoTopUp ? (
                    <Button color="inherit" size="small" onClick={() => setAutoTopUp(true)}>
                      Enable auto top-up
                    </Button>
                  ) : undefined
                }
              >
                {b.text}
              </Alert>
            ))}

            <Paper variant="outlined" sx={{ p: 3 }}>
              <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', mb: 1 }}>
                <Typography variant="subtitle1" sx={{ fontWeight: 700, textTransform: 'capitalize' }}>
                  {usage.tier} plan
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  {usage.period_end ? `Resets ${formatDate(usage.period_end)}` : ''}
                </Typography>
              </Box>
              <LinearProgress
                variant="determinate"
                value={Math.min(usage.percent_used, 100)}
                color={meterColor}
                sx={{ height: 10, borderRadius: 5, my: 1.5 }}
              />
              <Typography variant="body2">
                {formatBytes(usage.used_bytes)} of {formatBytes(usage.allowance_bytes)} used (
                {usage.percent_used.toFixed(1)}%) · {formatBytes(usage.remaining_bytes)} left
              </Typography>
              <Typography variant="caption" color="text.secondary">
                Connections: {usage.connection_slot_limit - usage.connection_slots_remaining} of{' '}
                {usage.connection_slot_limit} in use
              </Typography>
            </Paper>

            <Paper variant="outlined" sx={{ p: 3 }}>
              <Typography variant="subtitle1" sx={{ fontWeight: 700, mb: 1 }}>Data top-ups</Typography>
              {!usage.paid ? (
                <Typography variant="body2" color="text.secondary">
                  Top-ups are available on paid plans. On the free plan, gateway traffic stops when you
                  reach the limit.
                </Typography>
              ) : (
                <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                  <Typography variant="body2" color="text.secondary">
                    When your data runs out, we can automatically buy another{' '}
                    {formatBytes(usage.top_up_block_bytes)} for {formatCents(usage.top_up_price_cents)}.
                    Off by default.
                  </Typography>
                  <FormControlLabel
                    control={
                      <Switch
                        checked={usage.auto_top_up_enabled}
                        disabled={update.isPending || !usage.can_top_up}
                        onChange={(e) => setAutoTopUp(e.target.checked)}
                      />
                    }
                    label="Automatically buy a data top-up when I run out"
                  />
                  <Box sx={{ display: 'flex', gap: 2, alignItems: 'center' }}>
                    <AppTextField
                      label="Monthly auto top-up limit ($)"
                      type="number"
                      size="small"
                      value={capDraft ?? (usage.monthly_top_up_cap_cents / 100).toString()}
                      onChange={(e) => setCapDraft(e.target.value)}
                      helperText={`${formatCents(usage.top_up_spent_cents)} spent this period`}
                      sx={{ width: 240 }}
                    />
                    <Button variant="outlined" size="small" disabled={capDraft === null || update.isPending} onClick={saveCap}>
                      Save limit
                    </Button>
                  </Box>
                  <Box>
                    <Button
                      variant="contained"
                      disabled={!usage.can_top_up || buyTopUp.isPending}
                      startIcon={buyTopUp.isPending ? <CircularProgress size={14} /> : undefined}
                      onClick={() => run(() => buyTopUp.mutateAsync())}
                    >
                      Buy {formatBytes(usage.top_up_block_bytes)} now — {formatCents(usage.top_up_price_cents)}
                    </Button>
                  </Box>
                </Box>
              )}
            </Paper>
          </>
        )}
      </Box>
    </PageShell>
  );
}
