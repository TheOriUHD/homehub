import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath } from "node:url";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8080",
        ws: true,
      },
    },
  },
  build: {
    // noVNC 1.7 uses top-level await; the default es2020 target rejects it.
    target: "es2022",
    sourcemap: false,
    chunkSizeWarningLimit: 900,
  },
  optimizeDeps: {
    // The dev-server dependency prebundle needs the same target bump, or
    // `vite dev` fails on noVNC's top-level await.
    esbuildOptions: { target: "es2022" },
  },
});
