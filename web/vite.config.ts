import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// Built assets go straight into the Go embed directory.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  build: {
    outDir: "../internal/webui/dist",
    emptyOutDir: true,
    target: "es2022",
    chunkSizeWarningLimit: 500,
  },
  server: {
    port: 5173,
    proxy: { "/api": { target: process.env.ROARR_API ?? "http://localhost:3999", changeOrigin: true } },
  },
});
