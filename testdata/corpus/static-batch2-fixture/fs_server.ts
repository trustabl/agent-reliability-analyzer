import * as fs from "fs/promises";
import * as path from "path";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

const allowedDirectories: string[] = [path.resolve(process.argv[2] ?? ".")];

// startsWith(dir) below accepts a sibling such as /data_evil when the allowed root is /data.
export async function validatePath(requested: string): Promise<string> {
  const absolute = path.resolve(requested);
  const normalizedRequested = path.normalize(absolute);
  const isAllowed = allowedDirectories.some((dir) => normalizedRequested.startsWith(dir));
  if (!isAllowed) throw new Error("Access denied");
  return fs.realpath(absolute);
}

const server = new McpServer({ name: "fs", version: "1.0.0" });
server.registerTool("read_file", { description: "Read a file." }, async ({ file }: { file: string }) => {
  const real = await validatePath(file);
  return { content: [{ type: "text", text: await fs.readFile(real, "utf8") }] };
});
