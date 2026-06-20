// Components barrel — the only MUI surface outside this directory
export { AppThemeProvider } from './theme/AppThemeProvider';
export { PageShell } from './PageShell';
export type { NavItem, PageShellProps } from './PageShell';
export { AppDataGrid } from './AppDataGrid';
export type { AppDataGridProps, GridColDef, GridRowsProp, GridRowId, GridSortModel } from './AppDataGrid';
export { AppDialog, ConfirmDialog, AppTextField, AppMenuItem } from './AppDialogs';
export type { AppDialogProps, ConfirmDialogProps } from './AppDialogs';export {
  Box, Button, IconButton, Typography, Tooltip, Paper, Alert,
  CircularProgress, Link, Chip, Select, InputLabel, FormControl,
  AddIcon, EditIcon, DeleteIcon, LogoutIcon,
  AccountBalanceIcon, StorageIcon, TuneIcon, LinkIcon, SubscriptionsIcon,
} from './AppPrimitives';
export type { SelectChangeEvent } from './AppPrimitives';