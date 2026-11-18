# Source: original fixture

Authored for the trustabl test corpus. Synthetic LangChain (Python, TypeScript)
and OpenAI Agents projects wired to LangWatch, one sub-repo per case, scanned one
at a time by TestScan_LangWatchFixture. The SDK calls follow langwatch 1.4.0
(PyPI) and langwatch 2.2.0 (npm); the two `python-langchain-*` callback cases
follow LangWatch's LangChain integration guide (`create_agent` with the trace's
LangChain callback in `RunnableConfig`, and OpenInference's
`LangChainInstrumentor` passed to `langwatch.setup`). No keys: every reference reads
LANGWATCH_API_KEY from the environment. No external code. Public-domain.
