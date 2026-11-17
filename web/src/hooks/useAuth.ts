import { createContext, useContext } from "react";

export interface AuthState {
  /** True when the server asks for a login, so a sign-out makes sense. */
  authRequired: boolean;
  signOut: () => void;
}

/** Outside an AuthGate there is nothing to sign out of. */
export const AuthContext = createContext<AuthState>({
  authRequired: false,
  signOut: () => undefined,
});

export function useAuth(): AuthState {
  return useContext(AuthContext);
}
