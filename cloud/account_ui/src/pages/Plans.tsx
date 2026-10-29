import { useState } from 'react';
import { useAuth } from '../auth/AuthProvider';
import { useNavItems } from './useNavItems';
import {
  PageShell, Box, Button, Typography, Alert, CircularProgress,
  Card, CardContent, CardActions, Chip, CheckCircleIcon,
  IconButton, Tooltip, LogoutIcon,
} from '../components';
import {
  useCurrentBillingAccount, usePlans, usePlanPrices, useCreateCheckoutSession, useUsage,
} from '../datastore';
import { formatBytes, formatCents } from '../format';

// Module-level (not a component/hook) so the react-compiler lint rule
// doesn't mistake this for a mutation of component-reactive state --
// matches the one other window.location redirect in this codebase
// (api/client.ts's session-expiry redirect), which lives outside any
// component for the same reason.
function redirectToCheckout(url: string): void {
  window.location.href = url;
}

export function Plans() {
  const { user, logout } = useAuth();
  const navItems = useNavItems();

  const { billingAccount, isPending: billingAccountPending } = useCurrentBillingAccount();
  const { data: usage } = useUsage(billingAccount?.id);
  const { data: plans, isPending: plansPending, isError: plansError } = usePlans({ is_active: true });
  const { data: prices, isPending: pricesPending, isError: pricesError } = usePlanPrices({ is_active: true });

  const checkout = useCreateCheckoutSession(billingAccount?.id);
  const [selectingPriceId, setSelectingPriceId] = useState<string | null>(null);
  const [checkoutError, setCheckoutError] = useState<string | null>(null);

  const isPending = billingAccountPending || plansPending || pricesPending;

  const handleSubscribe = async (priceId: string) => {
    setCheckoutError(null);
    setSelectingPriceId(priceId);
    try {
      const session = await checkout.mutateAsync({ plan_price_id: priceId });
      redirectToCheckout(session.url);
    } catch (err) {
      setCheckoutError(err instanceof Error ? err.message : 'Failed to start checkout');
      setSelectingPriceId(null);
    }
  };

  const plansById = new Map((plans?.items ?? []).map((p) => [p.id, p]));
  const cards = (prices?.items ?? [])
    .filter((price) => plansById.has(price.plan_id))
    .sort((a, b) => a.unit_amount_cents - b.unit_amount_cents);

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
        <Typography variant="h6">Plans</Typography>
        <Typography variant="body2" color="text.secondary">
          Priced by data relayed through the gateway. Out of data? Buy a 100 GB top-up for $5 anytime
          — or turn on auto top-up under Usage.
        </Typography>
      </Box>

      <Box sx={{ px: 2, pb: 2, flex: 1 }}>
        {!billingAccountPending && !billingAccount && (
          <Alert severity="warning" sx={{ mb: 2 }}>
            No billing account found for your account.
          </Alert>
        )}
        {(plansError || pricesError) && <Alert severity="error" sx={{ mb: 2 }}>Failed to load plans.</Alert>}
        {checkoutError && <Alert severity="error" sx={{ mb: 2 }}>{checkoutError}</Alert>}
        {isPending && (
          <Box sx={{ display: 'flex', justifyContent: 'center', p: 4 }}>
            <CircularProgress size={28} />
          </Box>
        )}

        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 2 }}>
          {cards.map((price) => {
            const plan = plansById.get(price.plan_id);
            const isFree = price.unit_amount_cents === 0;
            const isCurrent = usage ? plan?.name === usage.tier : false;
            const isSelecting = selectingPriceId === price.id && checkout.isPending;
            return (
              <Card key={price.id} variant="outlined" sx={{ width: 260, borderColor: isCurrent ? 'primary.main' : undefined }}>
                <CardContent>
                  <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                    <Typography variant="subtitle1" sx={{ fontWeight: 700, textTransform: 'capitalize' }}>
                      {plan?.name ?? price.plan_id}
                    </Typography>
                    {isCurrent && <Chip label="Current" size="small" color="primary" />}
                  </Box>
                  <Typography variant="h5" sx={{ fontWeight: 700, my: 1 }}>
                    {isFree ? 'Free' : `${formatCents(price.unit_amount_cents, price.currency)} / ${price.interval}`}
                  </Typography>
                  <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.75, mt: 2 }}>
                    <Feature>{formatBytes(price.network_traffic_limit_bytes)} data / month</Feature>
                    {isFree ? (
                      <Feature>Traffic stops at the limit</Feature>
                    ) : (
                      <>
                        <Feature>{formatBytes(price.grace_bytes)} grace if you run out</Feature>
                        <Feature>
                          Top-ups: {formatBytes(price.top_up_block_bytes)} for {formatCents(price.top_up_price_cents)}
                        </Feature>
                      </>
                    )}
                  </Box>
                  {!isFree && !price.stripe_price_id && (
                    <Chip label="Not yet purchasable" size="small" sx={{ mt: 2 }} />
                  )}
                </CardContent>
                {!isFree && (
                  <CardActions sx={{ px: 2, pb: 2 }}>
                    <Button
                      variant="contained"
                      fullWidth
                      disabled={!billingAccount || !price.stripe_price_id || checkout.isPending || isCurrent}
                      startIcon={isSelecting ? <CircularProgress size={14} /> : undefined}
                      onClick={() => handleSubscribe(price.id)}
                    >
                      {isCurrent ? 'Current plan' : 'Subscribe'}
                    </Button>
                  </CardActions>
                )}
              </Card>
            );
          })}
        </Box>
      </Box>
    </PageShell>
  );
}

function Feature({ children }: { children: React.ReactNode }) {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
      <CheckCircleIcon fontSize="small" color="success" />
      <Typography variant="body2">{children}</Typography>
    </Box>
  );
}
