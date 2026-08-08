import { Hono } from "hono";

const app = new Hono();
const PORT = process.env.PORT || 3000;

app.get("/health", (c) => {
  return c.json({ status: "ok", timestamp: new Date().toISOString() });
});

app.post("/webhook", async (c) => {
  const body = await c.req.json();
  console.log("Webhook received:", body);
  return c.json({ received: true });
});

console.log(`Server running on http://localhost:${PORT}`);
export default { port: PORT, fetch: app.fetch };
