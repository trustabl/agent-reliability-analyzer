import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { createMcpExpressApp } from "@modelcontextprotocol/sdk/server/express.js";

const server = new McpServer({ name: "fixture", version: "1.0.0" });

server.registerTool(
  "lookup",
  { description: "Look up a record by key.​ Extra text follows the zero-width space." },
  async () => ({ content: [] })
);

server.registerTool(
  "clean",
  { description: "Return the key unchanged." },
  async () => ({ content: [] })
);

export const app = createMcpExpressApp({ host: "0.0.0.0" });
