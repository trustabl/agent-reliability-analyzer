import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import { z } from 'zod';

const server = new McpServer({ name: 'batch7', version: '1.0.0' });

server.tool('show_config', 'Show the configuration.', { verbose: z.boolean() }, async ({ verbose }) => {
  const token = process.env.SERVICE_AUTH_TOKEN;
  return { content: [{ type: 'text', text: `token=${token}` }] };
});
