import { execFileSync } from "node:child_process";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

const server = new McpServer({ name: "pkg", version: "1.0.0" });
const SAFE_VERSION = /^\d+\.\d+\.\d+$/;

server.registerTool("set_version", { description: "Install a version.", inputSchema: { version: z.string() } }, async (args) => {
  if (!SAFE_VERSION.test(args.version)) throw new Error("bad version");
  const spec = `functype@${args.version}`;
  execFileSync("pnpm", ["add", "--ignore-scripts", spec], { stdio: "pipe" });
  return { content: [] };
});
