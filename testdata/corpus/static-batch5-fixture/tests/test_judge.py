def judge_in_test(client):
    return client.chat.completions.create(model="gpt-4o-latest", messages=[])
