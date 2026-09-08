// Theme
export { AppThemeProvider } from './theme/AppThemeProvider';
export { theme } from './theme/theme';

// Layout
export { PageShell } from './PageShell';
export type { PageShellProps, NavItem } from './PageShell';

// Data
export { AppDataGrid } from './AppDataGrid';
export type { AppDataGridProps, GridColDef, GridRowsProp, GridRowId, GridSortModel, GridFilterModel } from './AppDataGrid';

// Forms
export { DeployForm } from './DeployForm';
export type { DeployFormProps, DeployFormValues, DeployFormFeedback } from './DeployForm';

// Icons (nav items reference these; re-exported so pages never import @mui/icons-material directly)
export { default as HubIcon } from '@mui/icons-material/Hub';
export { default as StorageIcon } from '@mui/icons-material/Storage';
export { default as RocketLaunchIcon } from '@mui/icons-material/RocketLaunch';
