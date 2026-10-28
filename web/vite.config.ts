/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The app is served by agentflight under /ui/. In development, Vite serves the
// same base and proxies API routes to a locally running `agentflight serve`.
const apiTarget = process.env.AGENTFLIGHT_API ?? "http://localhost:8080";
const apiRoutes = ["/runs", "/bench", "/healthz"];

export default defineConfig({
  base: "/ui/",
  plugins: [react()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    proxy: Object.fromEntries(
      apiRoutes.map((route) => [route, { target: apiTarget, changeOrigin: false }]),
    ),
  },
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
