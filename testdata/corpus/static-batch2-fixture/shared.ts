import express from "express";
import { McpServer } from "@modelcontextprotocol/server";
import { NodeStreamableHTTPServerTransport } from "@modelcontextprotocol/node";

const server = new McpServer({ name: "shared", version: "1.0.0" });
const app = express();

app.post("/mcp", async (req, res) => {
  const transport = new NodeStreamableHTTPServerTransport({ sessionIdGenerator: undefined });
  await server.connect(transport);
  await transport.handleRequest(req, res, req.body);
});
