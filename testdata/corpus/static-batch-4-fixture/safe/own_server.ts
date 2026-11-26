import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";

export const transport = new StdioClientTransport({
  command: "node",
  args: ["server.js"],
  env: process.env as Record<string, string>,
});
