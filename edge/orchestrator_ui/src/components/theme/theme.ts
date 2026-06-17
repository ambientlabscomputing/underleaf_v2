import { createTheme, alpha } from '@mui/material/styles';

// ── Palette tokens ────────────────────────────────────────────────────────────
const primary = {
  main: '#6366f1',   // indigo-500
  light: '#818cf8',  // indigo-400
  dark: '#4338ca',   // indigo-700
  contrastText: '#ffffff',
};

const secondary = {
  main: '#0ea5e9',   // sky-500
  light: '#38bdf8',
  dark: '#0284c7',
  contrastText: '#ffffff',
};

// ── Base theme (dark) ─────────────────────────────────────────────────────────
export const theme = createTheme({
  palette: {
    mode: 'dark',
    primary,
    secondary,
    background: {
      default: '#0f1117',
      paper: '#1a1d27',
    },
    divider: 'rgba(255,255,255,0.08)',
    text: {
      primary: '#f1f5f9',
      secondary: '#94a3b8',
      disabled: '#475569',
    },
    error:   { main: '#ef4444' },
    warning: { main: '#f59e0b' },
    success: { main: '#22c55e' },
    info:    { main: '#0ea5e9' },
  },

  // ── Typography ──────────────────────────────────────────────────────────────
  typography: {
    fontFamily: '"Inter", "system-ui", sans-serif',
    h1: { fontSize: '2rem',   fontWeight: 700, letterSpacing: '-0.02em' },
    h2: { fontSize: '1.5rem', fontWeight: 700, letterSpacing: '-0.01em' },
    h3: { fontSize: '1.25rem', fontWeight: 600 },
    h4: { fontSize: '1.125rem', fontWeight: 600 },
    h5: { fontSize: '1rem',   fontWeight: 600 },
    h6: { fontSize: '0.875rem', fontWeight: 600 },
    subtitle1: { fontSize: '0.875rem', fontWeight: 500, color: '#94a3b8' },
    subtitle2: { fontSize: '0.75rem',  fontWeight: 500, color: '#64748b' },
    body1: { fontSize: '0.875rem', lineHeight: 1.6 },
    body2: { fontSize: '0.8125rem', lineHeight: 1.5 },
    caption: { fontSize: '0.75rem', color: '#64748b' },
    overline: { fontSize: '0.6875rem', fontWeight: 600, letterSpacing: '0.08em' },
  },

  shape: { borderRadius: 8 },

  // ── Component overrides ─────────────────────────────────────────────────────
  components: {
    MuiCssBaseline: {
      styleOverrides: {
        body: {
          scrollbarColor: '#334155 transparent',
          '&::-webkit-scrollbar': { width: 6 },
          '&::-webkit-scrollbar-track': { background: 'transparent' },
          '&::-webkit-scrollbar-thumb': {
            background: '#334155',
            borderRadius: 3,
          },
        },
      },
    },

    MuiAppBar: {
      defaultProps: { elevation: 0 },
      styleOverrides: {
        root: {
          background: '#1a1d27',
          borderBottom: '1px solid rgba(255,255,255,0.06)',
        },
      },
    },

    MuiDrawer: {
      styleOverrides: {
        paper: {
          background: '#13161f',
          borderRight: '1px solid rgba(255,255,255,0.06)',
        },
      },
    },

    MuiButton: {
      defaultProps: { disableElevation: true },
      styleOverrides: {
        root: {
          textTransform: 'none',
          fontWeight: 500,
          borderRadius: 6,
        },
        containedPrimary: {
          '&:hover': { background: primary.light },
        },
      },
    },

    MuiIconButton: {
      styleOverrides: {
        root: {
          borderRadius: 6,
          '&:hover': { background: 'rgba(255,255,255,0.06)' },
        },
      },
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

    MuiListItemIcon: {
      styleOverrides: {
        root: { minWidth: 36, color: '#64748b' },
      },
    },

    MuiTooltip: {
      defaultProps: { arrow: true, placement: 'right' },
      styleOverrides: {
        tooltip: {
          background: '#334155',
          fontSize: '0.75rem',
        },
        arrow: { color: '#334155' },
      },
    },

    MuiPaper: {
      defaultProps: { elevation: 0 },
      styleOverrides: {
        root: { backgroundImage: 'none' },
        outlined: { borderColor: 'rgba(255,255,255,0.08)' },
      },
    },

    MuiDivider: {
      styleOverrides: {
        root: { borderColor: 'rgba(255,255,255,0.07)' },
      },
    },

    MuiChip: {
      styleOverrides: {
        root: { fontWeight: 500, borderRadius: 4 },
      },
    },
  },
});
