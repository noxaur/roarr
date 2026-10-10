import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Built assets go straight into the Go embed directory.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../internal/webui/dist",
    emptyOutDir: true,
    target: "es2022",
    chunkSizeWarningLimit: 400,
  },
  server: {
    port: 5173,
    proxy: { "/api": { target: process.env.ROARR_API ?? "http://localhost:3999", changeOrigin: true } },
  },
});
