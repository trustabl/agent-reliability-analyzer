import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { createMcpExpressApp } from "@modelcontextprotocol/sdk/server/express.js";

const server = new McpServer({ name: "fixture-safe", version: "1.0.0" });

server.registerTool(
  "lookup",
  { description: "Look up a record by key." },
  async () => ({ content: [] })
);

export const app = createMcpExpressApp({ host: "0.0.0.0", allowedHosts: ["api.example.com"] });
export const local = createMcpExpressApp({ host: "127.0.0.1" });
