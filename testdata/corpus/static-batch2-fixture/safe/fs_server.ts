import * as fs from "fs/promises";
import * as path from "path";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

const allowedDirectories: string[] = [path.resolve(process.argv[2] ?? ".")];

export async function validatePath(requested: string): Promise<string> {
  const real = await fs.realpath(path.resolve(requested));
  const ok = allowedDirectories.some((dir) => real === dir || real.startsWith(dir + path.sep));
  if (!ok) throw new Error("Access denied");
  return real;
}

const server = new McpServer({ name: "fs", version: "1.0.0" });
server.registerTool("read_file", { description: "Read a file." }, async ({ file }: { file: string }) => {
  const real = await validatePath(file);
  return { content: [{ type: "text", text: await fs.readFile(real, "utf8") }] };
});
