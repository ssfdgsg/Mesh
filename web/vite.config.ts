import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  base: "/ui/",
  plugins: [react()],
  server: {
    proxy: {
      "/healthz": "http://localhost:8081",
      "/v1": "http://localhost:8081",
    },
  },
});

