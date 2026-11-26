def judge_answer(client, text):
    return client.chat.completions.create(model="gpt-4o-2024-08-06", messages=[{"role": "user", "content": text}])
