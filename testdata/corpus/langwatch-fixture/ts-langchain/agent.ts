import { setupObservability } from "langwatch/observability/node";
import { createReactAgent } from "@langchain/langgraph/prebuilt";
import { ChatOpenAI } from "@langchain/openai";

// Reads LANGWATCH_API_KEY from the environment.
setupObservability({ serviceName: "order-agent" });

const llm = new ChatOpenAI({ model: "gpt-4o-mini" });
export const agent = createReactAgent({ llm, tools: [] });
