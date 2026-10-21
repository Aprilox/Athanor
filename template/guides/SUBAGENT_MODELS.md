
## Choosing your sub-agents' model

For every sub-agent you call, set the `model` parameter of the Agent tool yourself, for that call, from the difficulty of what you hand over (a sub-agent you call twice can get two different models):
- `haiku`: mechanical work with no judgement: renaming, formatting, moving files, running a known command and reporting its output, filling data from a clear format;
- `sonnet`: most of the work: code from a precise sheet, tests, screenshots and their comparison, reading and summarising, documentation, a bug whose cause is known;
- `opus`: judgement only: the architecture or a re-planning, a bug still not understood after two tries, a security review, the final critique of the product.

Pick the cheapest one that will do it right the first time: a redo costs more than the difference. Never a model above your own. Say the model you chose in the call's description, e.g. "tester (sonnet): checks M7".
