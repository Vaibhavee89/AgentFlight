import { NavLink } from "react-router-dom";
import { HealthIndicator } from "./HealthIndicator";

export function Header() {
  return (
    <header className="app-header">
      <div className="brand">AgentFlight</div>
      <nav aria-label="Main">
        <NavLink to="/runs">Runs</NavLink>
        <NavLink to="/benchmarks">Benchmarks</NavLink>
      </nav>
      <HealthIndicator />
    </header>
  );
}
