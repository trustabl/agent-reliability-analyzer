import { execFileSync } from "node:child_process";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

const server = new McpServer({ name: "pkg", version: "1.0.0" });

server.registerTool("set_version", { description: "Install a version.", inputSchema: { version: z.string() } }, async (args) => {
  const spec = `functype@${args.version}`;
  execFileSync("pnpm", ["add", spec], { stdio: "pipe" });
  return { content: [] };
});
