from transformers import AutoModelForCausalLM

model = AutoModelForCausalLM.from_pretrained("example-org/example-model", trust_remote_code=True)
