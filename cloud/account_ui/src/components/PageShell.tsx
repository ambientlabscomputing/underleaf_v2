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

// -- Types ---------------------------------------------------------------------

export interface NavItem {
  label: string;
  icon: React.ReactNode;
  onClick: () => void;
  selected?: boolean;
}

export interface PageShellProps {
  title?: string;
  navItems?: NavItem[];
  actions?: React.ReactNode;
  children: React.ReactNode;
}

// -- Constants -----------------------------------------------------------------

const DRAWER_WIDTH = 240;
const DRAWER_COLLAPSED_WIDTH = 56;

// -- Component -----------------------------------------------------------------

export function PageShell({ title = 'Cloud', navItems = [], actions, children }: PageShellProps) {
  const [open, setOpen] = useState(false);

  return (
    <Box sx={{ display: 'flex', height: '100vh', overflow: 'hidden' }}>
      <AppBar position="fixed" sx={{ zIndex: (t) => t.zIndex.drawer + 1 }}>
        <Toolbar variant="dense" sx={{ gap: 1 }}>
          <IconButton
            edge="start"
            color="inherit"
            size="small"
            aria-label={open ? 'close navigation' : 'open navigation'}
            onClick={() => setOpen((p) => !p)}
          >
            <MenuIcon fontSize="small" />
          </IconButton>
          <Typography variant="h6" noWrap sx={{ flexGrow: 1, fontWeight: 600, fontSize: '0.9375rem' }}>
            {title}
          </Typography>
          {actions}
        </Toolbar>
      </AppBar>

      <Drawer
        variant="permanent"
        sx={{
          width: open ? DRAWER_WIDTH : DRAWER_COLLAPSED_WIDTH,
          flexShrink: 0,
          whiteSpace: 'nowrap',
          transition: (t) => t.transitions.create('width', {
            easing: t.transitions.easing.sharp,
            duration: open ? t.transitions.duration.enteringScreen : t.transitions.duration.leavingScreen,
          }),
          '& .MuiDrawer-paper': {
            width: open ? DRAWER_WIDTH : DRAWER_COLLAPSED_WIDTH,
            overflowX: 'hidden',
            transition: (t) => t.transitions.create('width', {
              easing: t.transitions.easing.sharp,
              duration: open ? t.transitions.duration.enteringScreen : t.transitions.duration.leavingScreen,
            }),
          },
        }}
      >
        <Toolbar variant="dense" />
        <Divider />
        <List dense sx={{ pt: 1 }}>
          {navItems.map((item) => (
            <Tooltip key={item.label} title={open ? '' : item.label}>
              <ListItemButton
                selected={item.selected}
                onClick={item.onClick}
                sx={{ justifyContent: open ? 'initial' : 'center', px: 1.5 }}
              >
                <ListItemIcon sx={{ justifyContent: 'center', minWidth: open ? 36 : 'auto' }}>
                  {item.icon}
                </ListItemIcon>
                {open && (
                  <ListItemText
                    primary={item.label}
                    slotProps={{ primary: { variant: 'body2', sx: { fontWeight: 500 } } }}
                  />
                )}
              </ListItemButton>
            </Tooltip>
          ))}
        </List>
      </Drawer>

      <Box component="main" sx={{ flexGrow: 1, overflow: 'auto', display: 'flex', flexDirection: 'column' }}>
        <Toolbar variant="dense" />
        {children}
      </Box>
    </Box>
  );
}
