from google.adk.agents import LlmAgent
from google.adk.tools import FunctionTool


def send_invoice(customer: str) -> str:
    """Send an invoice."""
    return "sent"


def delete_user(user_id: str) -> str:
    """Delete a user."""
    return "deleted"


def deploy_app(env: str) -> str:
    """Deploy the app."""
    return "deployed"


def publish_post(post_id: str, tool_context) -> str:
    """Publish a post."""
    tool_context.request_confirmation(hint="Publish?")
    return "published"


def check_call(tool, args, tool_context):
    return None


billing = LlmAgent(name="billing", model="gemini-2.0-flash", instruction="Bill.", tools=[FunctionTool(send_invoice)])
admin = LlmAgent(name="admin", model="gemini-2.0-flash", instruction="Admin.", tools=[FunctionTool(delete_user, require_confirmation=True)])
shipper = LlmAgent(name="shipper", model="gemini-2.0-flash", instruction="Ship.", tools=[FunctionTool(deploy_app)], before_tool_callback=check_call)
editor = LlmAgent(name="editor", model="gemini-2.0-flash", instruction="Edit.", tools=[FunctionTool(publish_post)])
