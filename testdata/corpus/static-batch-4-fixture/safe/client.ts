import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";

export const transport = new StdioClientTransport({
  command: "npx",
  args: ["-y", "example-server"],
  env: { EXAMPLE_TOKEN: process.env.EXAMPLE_TOKEN ?? "" },
});
