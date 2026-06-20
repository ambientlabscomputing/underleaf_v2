import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
import { authService, type User } from '../api/services/AuthService';
import { clearTokens, getAccessToken } from '../api/client';

// -- Types ---------------------------------------------------------------------

interface AuthContextValue {
  user: User | null;
  loading: boolean;
  login: (email: string, password: string) => Promise<void>;
  logout: () => void;
}

// -- Context -------------------------------------------------------------------

const AuthContext = createContext<AuthContextValue | null>(null);

// -- Provider ------------------------------------------------------------------

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(() => !!getAccessToken());

  // On mount: if there's an access token, try to load the current user
  useEffect(() => {
    if (!getAccessToken()) return;
    authService.userinfo()
      .then(setUser)
      .catch(() => {
        clearTokens();
        setUser(null);
      })
      .finally(() => setLoading(false));
  }, []);

  const login = async (email: string, password: string) => {
    await authService.login(email, password);
    const u = await authService.userinfo();
    setUser(u);
  };

  const logout = () => {
    clearTokens();
    setUser(null);
  };

  return (
    <AuthContext.Provider value={{ user, loading, login, logout }}>
      {children}
    </AuthContext.Provider>
  );
}

// -- Hook ----------------------------------------------------------------------

// AuthProvider and useAuth are intentionally co-located per auth context pattern.
// eslint-disable-next-line react-refresh/only-export-components
export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used inside <AuthProvider>');
  return ctx;
}
