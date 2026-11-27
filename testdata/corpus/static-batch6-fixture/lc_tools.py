from langchain_core.tools import tool


@tool
def search_docs(query: str, tenant: str) -> str:
    """Search the tenant's documents."""
    docs = vectorstore.similarity_search(query, filter={"tenant": tenant})
    return str(docs)


@tool
def recall(user: str, key: str) -> str:
    """Recall a stored item."""
    return str(store.get(("memories", user), key))
