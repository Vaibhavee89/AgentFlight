import { Navigate, Route, Routes } from "react-router-dom";
import { Header } from "./components/Header";
import { BenchmarksPage } from "./pages/BenchmarksPage";
import { NotFoundPage } from "./pages/NotFoundPage";
import { RunsPage } from "./pages/RunsPage";

export function App() {
  return (
    <>
      <Header />
      <main className="app-main">
        <Routes>
          <Route path="/" element={<Navigate to="/runs" replace />} />
          <Route path="/runs" element={<RunsPage />} />
          <Route path="/benchmarks" element={<BenchmarksPage />} />
          <Route path="*" element={<NotFoundPage />} />
        </Routes>
      </main>
    </>
  );
}
