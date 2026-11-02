import { defineConfig } from "vite";
export default defineConfig({
  base: "/community/",
  build: { outDir: "../public/community", emptyOutDir: true },
  server: {
    proxy: {
      "/api": "http://127.0.0.1:8090",
      "/ws": { target: "ws://127.0.0.1:8090", ws: true },
    },
  },
});
