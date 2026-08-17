import { createContext, useContext, useEffect, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, isApiError, type Me } from "./api";
import { evaluate, type Level } from "./perms";
import { live } from "./ws";

export type AuthState = {
  me: Me | null;
  loading: boolean;
  /** can mirrors server-side permission evaluation for rendering decisions. */
  can: (resource: string, level?: Level) => boolean;
  refresh: () => Promise<void>;
  signOut: () => Promise<void>;
};

const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();

  const meQuery = useQuery({
    queryKey: ["me"],
    queryFn: async () => {
      try {
        return unwrap(await api.GET("/auth/me"));
      } catch (err) {
        if (isApiError(err) && err.status === 401) return null;
        throw err;
      }
    },
    staleTime: 60_000,
    retry: (count, err) => !(isApiError(err) && err.status === 401) && count < 2,
  });

  const me = meQuery.data ?? null;

  // Hold the live socket open exactly while signed in.
  useEffect(() => {
    if (me) {
      live.connect();
      return () => live.disconnect();
    }
  }, [me?.user.id]);

  const value: AuthState = {
    me,
    loading: meQuery.isLoading,
    can: (resource, level = "view") => (me ? evaluate(me.grants, resource, level) : false),
    refresh: async () => {
      await queryClient.invalidateQueries({ queryKey: ["me"] });
    },
    signOut: async () => {
      await api.POST("/auth/logout");
      live.disconnect();
      queryClient.clear();
      window.location.href = "/login";
    },
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth outside AuthProvider");
  return ctx;
}
