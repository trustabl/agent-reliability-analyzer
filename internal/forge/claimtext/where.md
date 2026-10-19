Put the step where the SDK gives control before a tool runs. When the SDK has
no such hook, put it in one shared wrapper that every tool goes through, as
the first thing it does — never copied into each tool by hand.
