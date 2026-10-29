import { readdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { brotliCompressSync, constants, gzipSync } from "node:zlib";
import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

const outDir = fileURLToPath(new URL("../internal/webui/dist", import.meta.url));

// The fonts the first screen paints with. Preloading them starts the download
// with the HTML instead of after the CSS has been parsed.
const FIRST_FONTS = [/^assets\/inter-latin-opsz-normal-.*\.woff2$/, /^assets\/dm-mono-latin-400-normal-.*\.woff2$/];

function preloadFonts(): Plugin {
  return {
    name: "roarr-preload-fonts",
    apply: "build",
    transformIndexHtml: {
      order: "post",
      handler(_html, ctx) {
        const files = Object.keys(ctx.bundle ?? {});
        return FIRST_FONTS.flatMap((re) => files.filter((f) => re.test(f))).map((f) => ({
          tag: "link",
          attrs: { rel: "preload", href: `/${f}`, as: "font", type: "font/woff2", crossorigin: "" },
          injectTo: "head" as const,
        }));
      },
    },
  };
}

// roarr serves these files from the binary as they are. Compressing them once
// at build time costs nothing per request (see server.static).
function precompress(): Plugin {
  const walk = (dir: string): string[] =>
    readdirSync(dir).flatMap((n) => {
      const p = join(dir, n);
      return statSync(p).isDirectory() ? walk(p) : [p];
    });
  return {
    name: "roarr-precompress",
    apply: "build",
    closeBundle() {
      for (const f of walk(outDir)) {
        if (!/\.(js|css|html|svg|json)$/.test(f)) continue;
        const raw = readFileSync(f);
        if (raw.length < 1024) continue;
        writeFileSync(`${f}.br`, brotliCompressSync(raw, { params: { [constants.BROTLI_PARAM_QUALITY]: 11, [constants.BROTLI_PARAM_SIZE_HINT]: raw.length } }));
        writeFileSync(`${f}.gz`, gzipSync(raw, { level: 9 }));
      }
    },
  };
}

// Built assets go straight into the Go embed directory.
export default defineConfig({
  plugins: [react(), tailwindcss(), preloadFonts(), precompress()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  build: {
    outDir,
    emptyOutDir: true,
    target: "es2022",
    chunkSizeWarningLimit: 500,
    rollupOptions: {
      output: {
        // Libraries change less often than roarr: a release that only
        // touches roarr's code leaves this chunk in the browser cache.
        // React only: other libraries are split by the pages that use them.
        manualChunks: (id) => (/\/node_modules\/(\.pnpm\/)?(react|react-dom|scheduler)\//.test(id) ? "vendor" : undefined),
      },
    },
  },
  server: {
    port: 5173,
    proxy: { "/api": { target: process.env.ROARR_API ?? "http://localhost:3999", changeOrigin: true } },
  },
});
