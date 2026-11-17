import { NavLink } from "react-router-dom";
import { useAuth } from "../hooks/useAuth";
import { HealthIndicator } from "./HealthIndicator";

export function Header() {
  const { authRequired, signOut } = useAuth();
  return (
    <header className="app-header">
      <div className="brand">AgentFlight</div>
      <nav aria-label="Main">
        <NavLink to="/runs">Runs</NavLink>
        <NavLink to="/benchmarks">Benchmarks</NavLink>
      </nav>
      <HealthIndicator />
      {authRequired && (
        <button type="button" className="button button-small" onClick={signOut}>
          Sign out
        </button>
      )}
    </header>
  );
}
