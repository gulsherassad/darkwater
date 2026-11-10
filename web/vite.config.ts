import { defineConfig } from "vite";

export default defineConfig({
  server: {
    proxy: {
      "/api": {
        target: "http://localhost:8080",
        ws: true, // also forward WebSocket connections
      },
    },
  },
  optimizeDeps: {
    // MapLibre loads its own worker file; pre-bundling breaks that path
    exclude: ["maplibre-gl"],
  },
});