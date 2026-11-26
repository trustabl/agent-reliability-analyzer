import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";

const serverConfig = { command: "npx" };

export const transport = new StdioClientTransport({
  command: serverConfig.command,
  args: ["-y", "example-server"],
  env: process.env as Record<string, string>,
});
