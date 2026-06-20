import { createTheme, alpha } from '@mui/material/styles';

const primary = {
  main: '#6366f1',
  light: '#818cf8',
  dark: '#4338ca',
  contrastText: '#ffffff',
};

const secondary = {
  main: '#0ea5e9',
  light: '#38bdf8',
  dark: '#0284c7',
  contrastText: '#ffffff',
};

export const theme = createTheme({
  palette: {
    mode: 'dark',
    primary,
    secondary,
    background: { default: '#0f1117', paper: '#1a1d27' },
    divider: 'rgba(255,255,255,0.08)',
    text: { primary: '#f1f5f9', secondary: '#94a3b8', disabled: '#475569' },
    error:   { main: '#ef4444' },
    warning: { main: '#f59e0b' },
    success: { main: '#22c55e' },
    info:    { main: '#0ea5e9' },
  },
  typography: {
    fontFamily: '"Inter", "system-ui", sans-serif',
    h6: { fontSize: '0.875rem', fontWeight: 600 },
    body1: { fontSize: '0.875rem', lineHeight: 1.6 },
    body2: { fontSize: '0.8125rem', lineHeight: 1.5 },
  },
  shape: { borderRadius: 8 },
  components: {
    MuiCssBaseline: {
      styleOverrides: {
        body: {
          scrollbarColor: '#334155 transparent',
          '&::-webkit-scrollbar': { width: 6 },
          '&::-webkit-scrollbar-thumb': { background: '#334155', borderRadius: 3 },
        },
      },
    },
    MuiAppBar: {
      defaultProps: { elevation: 0 },
      styleOverrides: { root: { background: '#1a1d27', borderBottom: '1px solid rgba(255,255,255,0.06)' } },
    },
    MuiDrawer: {
      styleOverrides: { paper: { background: '#13161f', borderRight: '1px solid rgba(255,255,255,0.06)' } },
    },
    MuiButton: {
      defaultProps: { disableElevation: true },
      styleOverrides: { root: { textTransform: 'none', fontWeight: 500, borderRadius: 6 } },
    },
    MuiListItemButton: {
      styleOverrides: {
        root: {
          borderRadius: 6,
          margin: '1px 8px',
          width: 'calc(100% - 16px)',
          '&.Mui-selected': {
            background: alpha(primary.main, 0.15),
            color: primary.light,
            '&:hover': { background: alpha(primary.main, 0.22) },
            '& .MuiListItemIcon-root': { color: primary.light },
          },
          '&:hover': { background: 'rgba(255,255,255,0.05)' },
        },
      },
    },
    MuiListItemIcon: { styleOverrides: { root: { minWidth: 36, color: '#64748b' } } },
    MuiPaper: {
      defaultProps: { elevation: 0 },
      styleOverrides: { root: { backgroundImage: 'none' } },
    },
    MuiTooltip: {
      defaultProps: { arrow: true, placement: 'right' },
      styleOverrides: {
        tooltip: { background: '#334155', fontSize: '0.75rem' },
        arrow: { color: '#334155' },
      },
    },
  },
});
