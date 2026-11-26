/**
 * OpenAI Multi-Agent Research Example
 *
 * Demonstrates OpenAI instrumentation with a sequential research workflow
 * that mirrors the Python CrewAI example: research → analysis → blog post.
 */

import { randomUUID } from "crypto";
import OpenAI from "openai";
import { AgentGuard } from "@timeplus/agentguard";
import { OpenAIInstrumentation } from "@arizeai/openinference-instrumentation-openai";

const baseURL = "https://openrouter.ai/api/v1";
const apiKey = "";
const model = "openrouter/free";

const ag = AgentGuard.getInstance({
  serviceName: "agentguard-sdk",
  agentId: "openai-demo",
  deploymentId: "openai-1",
  deploymentName: "OpenAI Research Workflow",
  otlpEndpoint: "http://localhost:4318",
});

// Instrument OpenAI
const openaiInstrumentation = new OpenAIInstrumentation();
openaiInstrumentation.setTracerProvider(ag.provider);
openaiInstrumentation.manuallyInstrument(OpenAI);

const client = new OpenAI({
  baseURL: baseURL,
  apiKey: apiKey,
});

// --- simulated tools ---

function searchTool(query: string, maxResults = 5): string {
  return `Search results for '${query}':
1. Recent advancements in ${query} show significant progress in 2024
2. Industry leaders report ${query} as a top priority
3. Research papers on ${query} increased by 45% this year
4. Key applications of ${query} in enterprise environments
5. Future trends and predictions for ${query}
(Showing top ${maxResults} results)`;
}

function dataAnalysisTool(data: string, analysisType = "summary"): string {
  const wordCount = data.split(/\s+/).length;
  return `Data Analysis (${analysisType}):
- Total words analyzed: ${wordCount}
- Key themes identified: AI, Machine Learning, Innovation
- Sentiment: Positive (85%)
- Trend direction: Upward
- Confidence score: High
- Recommendation: Strong potential for further research`;
}

function contentFormatterTool(content: string, formatType = "blog"): string {
  if (formatType === "blog") {
    return `# Blog Post\n\n${content}\n\n---\n*Published by AI Research Team*\n*Tags: #AI #Technology #Innovation*\n`;
  }
  if (formatType === "report") {
    return `RESEARCH REPORT\n===============\n\n${content}\n\nCONCLUSIONS\n-----------\nThe findings indicate significant progress.\n`;
  }
  return `Summary: ${content.slice(0, 200)}...`;
}

// --- agent steps ---

async function researchAgent(topic: string): Promise<string> {
  const searchResults = searchTool(`${topic} 2024`);
  const response = await client.chat.completions.create({
    model: model,
    messages: [
      {
        role: "system",
        content:
          "You are a Senior Research Analyst. Summarise the provided search results into a concise bullet-point report.",
      },
      {
        role: "user",
        content: `Search results:\n${searchResults}\n\nProvide an analysis report in bullet points.`,
      },
    ],
  });
  return response.choices[0].message.content ?? "";
}

async function dataScientistAgent(researchReport: string): Promise<string> {
  const analysisResults = dataAnalysisTool(researchReport, "trends");
  const response = await client.chat.completions.create({
    model: model,
    messages: [
      {
        role: "system",
        content:
          "You are an AI Data Scientist. Interpret the tool output and provide data-driven insights.",
      },
      {
        role: "user",
        content: `Research report:\n${researchReport}\n\nAnalysis tool output:\n${analysisResults}\n\nProvide a technical analysis.`,
      },
    ],
  });
  return response.choices[0].message.content ?? "";
}

async function contentWriterAgent(analysis: string): Promise<string> {
  const draft = await client.chat.completions.create({
    model: model,
    messages: [
      {
        role: "system",
        content:
          "You are a Tech Content Strategist. Transform the analysis into an engaging blog post draft (2–3 paragraphs).",
      },
      { role: "user", content: analysis },
    ],
  });
  const draftText = draft.choices[0].message.content ?? "";
  return contentFormatterTool(draftText, "blog");
}

// --- orchestration ---

async function runResearchWorkflow(topic: string): Promise<void> {
  await ag.withSpan(
    "runResearchWorkflow",
    { sessionId: randomUUID() },
    async (rootSpan) => {
      console.log(`\n${"=".repeat(60)}`);
      console.log(`Research workflow: "${topic}"`);
      console.log("=".repeat(60));

      const report = await ag.withSpan(
        "research",
        { runId: randomUUID() },
        () => researchAgent(topic),
      );
      console.log("\n[Research Report]\n", report);

      const analysis = await ag.withSpan(
        "analysis",
        { runId: randomUUID() },
        () => dataScientistAgent(report),
      );
      console.log("\n[Data Analysis]\n", analysis);

      const blogPost = await ag.withSpan(
        "writing",
        { runId: randomUUID() },
        () => contentWriterAgent(analysis),
      );
      console.log("\n[Blog Post]\n", blogPost);

      rootSpan.setAttribute("workflow.topic", topic);
      console.log("\nWorkflow complete.");
    },
  );
}

async function main(): Promise<void> {
  await runResearchWorkflow("AI advancements");
  await ag.shutdown();
}

main().catch(console.error);
