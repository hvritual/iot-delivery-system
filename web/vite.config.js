import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The Yunka MVP uses a parallel port by default, while the environment override
// keeps the old backend or a remote target selectable without source edits.
export const deliveryApiTarget = process.env.IOT_DELIVERY_API_TARGET ?? "http://127.0.0.1:8281";

export function buildDeliveryProxy(target, apiKey) {
  const key = String(apiKey ?? "").trim();
  return {
    target,
    changeOrigin: true,
    ...(key ? { headers: { "X-API-Key": key } } : {}),
  };
}

const deliveryProxy = buildDeliveryProxy(deliveryApiTarget, process.env.IOT_DELIVERY_LOCAL_API_KEY);
const healthProxy = { target: deliveryApiTarget, changeOrigin: true };

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": deliveryProxy,
      "/health": healthProxy,
    },
  },
});
