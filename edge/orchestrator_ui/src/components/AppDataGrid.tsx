import {
  DataGrid as MuiDataGrid,
  type DataGridProps,
  type GridColDef,
  type GridRowsProp,
  type GridRowId,
  type GridSortModel,
  type GridFilterModel,
} from '@mui/x-data-grid';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';

// Re-export grid types so the rest of the app never imports from @mui/x-data-grid directly
export type { GridColDef, GridRowsProp, GridRowId, GridSortModel, GridFilterModel };

// ── Slots ─────────────────────────────────────────────────────────────────────

function NoRowsOverlay({ message = 'No data' }: { message?: string }) {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
      <Typography variant="body2" color="text.secondary">
        {message}
      </Typography>
    </Box>
  );
}

function LoadingOverlay() {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
      <CircularProgress size={28} />
    </Box>
  );
}

// ── Props ─────────────────────────────────────────────────────────────────────

export interface AppDataGridProps extends Omit<DataGridProps, 'slots'> {
  /** Message shown when the grid has no rows */
  emptyMessage?: string;
}

// ── Component ─────────────────────────────────────────────────────────────────

export function AppDataGrid({ emptyMessage, sx, ...props }: AppDataGridProps) {
  return (
    <MuiDataGrid
      {...props}
      density="compact"
      disableRowSelectionOnClick
      pageSizeOptions={[25, 50, 100]}
      initialState={{
        pagination: { paginationModel: { pageSize: 25 } },
        ...props.initialState,
      }}
      slots={{
        noRowsOverlay: () => <NoRowsOverlay message={emptyMessage} />,
        loadingOverlay: LoadingOverlay,
      }}
      sx={{
        border: 'none',
        '& .MuiDataGrid-columnHeaders': {
          background: 'rgba(255,255,255,0.03)',
          borderBottom: '1px solid rgba(255,255,255,0.08)',
        },
        '& .MuiDataGrid-columnHeaderTitle': {
          fontWeight: 600,
          fontSize: '0.75rem',
          textTransform: 'uppercase',
          letterSpacing: '0.06em',
          color: 'text.secondary',
        },
        '& .MuiDataGrid-row': {
          '&:hover': { background: 'rgba(255,255,255,0.03)' },
          '&.Mui-selected': {
            background: 'rgba(99,102,241,0.12)',
            '&:hover': { background: 'rgba(99,102,241,0.18)' },
          },
        },
        '& .MuiDataGrid-cell': {
          borderBottom: '1px solid rgba(255,255,255,0.05)',
          fontSize: '0.8125rem',
        },
        '& .MuiDataGrid-footerContainer': {
          borderTop: '1px solid rgba(255,255,255,0.08)',
        },
        ...sx,
      }}
    />
  );
}
