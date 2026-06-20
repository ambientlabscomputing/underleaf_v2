import { useNavigate, useLocation } from 'react-router-dom';
import {
  AccountBalanceIcon, StorageIcon, TuneIcon, LinkIcon, SubscriptionsIcon,
} from '../components';
import type { NavItem } from '../components';

// -- Component -----------------------------------------------------------------
// Note: This file lives in pages/ and imports from @mui/icons-material.
// Icons are MUI but only icons — no MUI components are used outside components/.
// This is acceptable per the standard; the eslint rule targets @mui/material and
// @mui/x-data-grid, not @mui/icons-material (which are SVG-only).

export function useNavItems(): NavItem[] {
  const navigate = useNavigate();
  const location = useLocation();

  return [
    { label: 'Billing Accounts', icon: <AccountBalanceIcon fontSize="small" />, onClick: () => navigate('/billing-accounts'), selected: location.pathname === '/billing-accounts' },
    { label: 'Clusters',         icon: <StorageIcon fontSize="small" />,         onClick: () => navigate('/clusters'),          selected: location.pathname === '/clusters' },
    { label: 'Tunnels',          icon: <TuneIcon fontSize="small" />,            onClick: () => navigate('/tunnels'),           selected: location.pathname === '/tunnels' },
    { label: 'Connections',      icon: <LinkIcon fontSize="small" />,            onClick: () => navigate('/connections'),       selected: location.pathname === '/connections' },
    { label: 'Subscriptions',    icon: <SubscriptionsIcon fontSize="small" />,   onClick: () => navigate('/subscriptions'),     selected: location.pathname === '/subscriptions' },
  ];
}
