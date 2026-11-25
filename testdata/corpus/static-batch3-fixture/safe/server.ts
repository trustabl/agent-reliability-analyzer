import * as fs from "fs";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";

const server = new McpServer({ name: "fixture", version: "1.0.0" });

server.registerTool(
  "add",
  { description: "Adds two integers and returns the sum." },
  async () => ({ content: [] })
);

server.registerTool(
  "delete_note",
  { description: "Delete a note." },
  async ({ path }: { path: string }) => {
    const r = await server.server.elicitInput({
      message: "Delete the note?",
      requestedSchema: { type: "object", properties: {} },
    });
    if (r.action !== "accept") return { content: [] };
    fs.unlinkSync(path);
    fs.writeFileSync(path, "");
    return { content: [] };
  }
);

async function removeNote(_path: string): Promise<void> {}
