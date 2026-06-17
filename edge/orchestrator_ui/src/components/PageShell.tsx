import { useState } from 'react';
import AppBar from '@mui/material/AppBar';
import Box from '@mui/material/Box';
import Drawer from '@mui/material/Drawer';
import IconButton from '@mui/material/IconButton';
import List from '@mui/material/List';
import ListItemButton from '@mui/material/ListItemButton';
import ListItemIcon from '@mui/material/ListItemIcon';
import ListItemText from '@mui/material/ListItemText';
import Toolbar from '@mui/material/Toolbar';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import Divider from '@mui/material/Divider';
import MenuIcon from '@mui/icons-material/Menu';

// ── Types ─────────────────────────────────────────────────────────────────────

export interface NavItem {
  label: string;
  icon: React.ReactNode;
  onClick: () => void;
  selected?: boolean;
}

export interface PageShellProps {
  /** Page title shown in the AppBar */
  title?: string;
  /** Navigation items rendered in the side drawer */
  navItems?: NavItem[];
  /** Slot for AppBar action buttons (right side) */
  actions?: React.ReactNode;
  children: React.ReactNode;
}

// ── Constants ─────────────────────────────────────────────────────────────────

const DRAWER_WIDTH = 240;
const DRAWER_COLLAPSED_WIDTH = 56;

// ── Component ─────────────────────────────────────────────────────────────────

export function PageShell({ title = 'Orchestrator', navItems = [], actions, children }: PageShellProps) {
  const [open, setOpen] = useState(false);

  const toggleDrawer = () => setOpen((prev) => !prev);

  return (
    <Box sx={{ display: 'flex', height: '100vh', overflow: 'hidden' }}>
      {/* ── AppBar ── */}
      <AppBar
        position="fixed"
        sx={{ zIndex: (theme) => theme.zIndex.drawer + 1 }}
      >
        <Toolbar variant="dense" sx={{ gap: 1 }}>
          <IconButton
            edge="start"
            color="inherit"
            aria-label={open ? 'close navigation' : 'open navigation'}
            onClick={toggleDrawer}
            size="small"
          >
            <MenuIcon fontSize="small" />
          </IconButton>

          <Typography variant="h6" noWrap sx={{ flexGrow: 1, fontWeight: 600, fontSize: '0.9375rem' }}>
            {title}
          </Typography>

          {actions}
        </Toolbar>
      </AppBar>

      {/* ── Side Drawer ── */}
      <Drawer
        variant="permanent"
        sx={{
          width: open ? DRAWER_WIDTH : DRAWER_COLLAPSED_WIDTH,
          flexShrink: 0,
          whiteSpace: 'nowrap',
          transition: (theme) =>
            theme.transitions.create('width', {
              easing: theme.transitions.easing.sharp,
              duration: open
                ? theme.transitions.duration.enteringScreen
                : theme.transitions.duration.leavingScreen,
            }),
          '& .MuiDrawer-paper': {
            width: open ? DRAWER_WIDTH : DRAWER_COLLAPSED_WIDTH,
            overflowX: 'hidden',
            transition: (theme) =>
              theme.transitions.create('width', {
                easing: theme.transitions.easing.sharp,
                duration: open
                  ? theme.transitions.duration.enteringScreen
                  : theme.transitions.duration.leavingScreen,
              }),
          },
        }}
      >
        {/* Spacer to push content below AppBar */}
        <Toolbar variant="dense" />

        <Divider />

        <List dense sx={{ pt: 1 }}>
          {navItems.map((item) => (
            <Tooltip key={item.label} title={open ? '' : item.label} placement="right">
              <ListItemButton
                selected={item.selected}
                onClick={item.onClick}
                sx={{ justifyContent: open ? 'initial' : 'center', px: 1.5 }}
              >
                <ListItemIcon sx={{ justifyContent: 'center', minWidth: open ? 36 : 'auto' }}>
                  {item.icon}
                </ListItemIcon>
                {open && <ListItemText primary={item.label} primaryTypographyProps={{ variant: 'body2', fontWeight: 500 }} />}
              </ListItemButton>
            </Tooltip>
          ))}
        </List>
      </Drawer>

      {/* ── Main Content ── */}
      <Box
        component="main"
        sx={{
          flexGrow: 1,
          overflow: 'auto',
          display: 'flex',
          flexDirection: 'column',
        }}
      >
        <Toolbar variant="dense" />
        {children}
      </Box>
    </Box>
  );
}
