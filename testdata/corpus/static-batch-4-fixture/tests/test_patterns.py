import os

from mcp import StdioServerParameters
from transformers import AutoModelForCausalLM

params = StdioServerParameters(command="x", env=os.environ)
model = AutoModelForCausalLM.from_pretrained("example-org/example-model", trust_remote_code=True)
validate_url_for_ssrf("http://example.test", warn_only=True)
