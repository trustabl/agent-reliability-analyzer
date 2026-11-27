import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { execSync } from "node:child_process";
import { z } from "zod";

const server = new McpServer({ name: "batch6", version: "1.0.0" });

server.tool("git_show", { ref: z.string() }, async ({ ref }) => {
  const out = execSync("git show " + ref).toString();
  return { content: [{ type: "text", text: out }] };
});

server.tool("calc", { expr: z.string() }, async ({ expr }) => {
  return { content: [{ type: "text", text: String(eval(expr)) }] };
});
