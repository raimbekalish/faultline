import { defineConfig } from "vitest/config";
export default defineConfig({
  server: { host: "127.0.0.1" },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test-setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
