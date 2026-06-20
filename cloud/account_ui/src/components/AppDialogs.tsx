import Dialog from '@mui/material/Dialog';
import DialogTitle from '@mui/material/DialogTitle';
import DialogContent from '@mui/material/DialogContent';
import DialogActions from '@mui/material/DialogActions';
import Button from '@mui/material/Button';
import TextField from '@mui/material/TextField';
import MenuItem from '@mui/material/MenuItem';
import CircularProgress from '@mui/material/CircularProgress';
import Alert from '@mui/material/Alert';

// Re-export MUI primitives needed in pages
export { TextField as AppTextField, MenuItem as AppMenuItem };

// -- AppDialog -----------------------------------------------------------------

export interface AppDialogProps {
  open: boolean;
  title: string;
  submitLabel?: string;
  loading?: boolean;
  error?: string | null;
  onClose: () => void;
  onSubmit: () => void;
  children: React.ReactNode;
}

export function AppDialog({
  open,
  title,
  submitLabel = 'Save',
  loading = false,
  error,
  onClose,
  onSubmit,
  children,
}: AppDialogProps) {
  return (
    <Dialog open={open} onClose={onClose} fullWidth maxWidth="sm">
      <DialogTitle sx={{ fontWeight: 600 }}>{title}</DialogTitle>
      <DialogContent sx={{ display: 'flex', flexDirection: 'column', gap: 2, pt: '16px !important' }}>
        {error && <Alert severity="error">{error}</Alert>}
        {children}
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose} disabled={loading}>Cancel</Button>
        <Button variant="contained" onClick={onSubmit} disabled={loading} startIcon={loading ? <CircularProgress size={14} /> : undefined}>
          {submitLabel}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

// -- ConfirmDialog -------------------------------------------------------------

export interface ConfirmDialogProps {
  open: boolean;
  title: string;
  message: string;
  loading?: boolean;
  onClose: () => void;
  onConfirm: () => void;
}

export function ConfirmDialog({ open, title, message, loading = false, onClose, onConfirm }: ConfirmDialogProps) {
  return (
    <Dialog open={open} onClose={onClose} maxWidth="xs" fullWidth>
      <DialogTitle sx={{ fontWeight: 600 }}>{title}</DialogTitle>
      <DialogContent>
        <span style={{ fontSize: '0.875rem' }}>{message}</span>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose} disabled={loading}>Cancel</Button>
        <Button variant="contained" color="error" onClick={onConfirm} disabled={loading}
          startIcon={loading ? <CircularProgress size={14} /> : undefined}>
          Delete
        </Button>
      </DialogActions>
    </Dialog>
  );
}
