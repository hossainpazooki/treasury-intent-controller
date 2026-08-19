# Python 3.14 deferred annotations: a function-local pydantic model breaks LangChain tool-schema building

ts: 2026-08-18T21:05:00Z
commit: intent-plane 9bf8d16 + uncommitted working tree (the adapter TDD; approximate ts — clock not read at capture, but the basis landed between the skeptic reports of 20:36Z and the adapter lane going green)
session: 78bbff6c-2e2c-4e7f-b4ee-9fa22af6ee36
status: verified
fact: Under Python 3.14 (PEP 649 deferred annotation evaluation via
annotationlib), a pydantic model class defined INSIDE a test function cannot
be used as a parameter annotation on a `@tool`-decorated function: when
langchain-core builds the args schema it resolves the annotations after the
local scope is gone and raises NameError. The same model at module level
works. This will bite any 3.14 codebase that defines per-test models for
tool signatures.
basis: "core/scorer/.venv/Scripts/python -m pytest declarant/pydeclarant/test_langchain_adapter.py -q" -> "E  NameError: name 'Leg' is not defined" at "annotationlib.py:201", 1 failed / 12 passed; moving `class Leg(BaseModel)` to module level -> 13 passed.
re-verify: `grep -n "module-level: py3.14 deferred annotations" ~/dev/intent-plane/declarant/pydeclarant/test_langchain_adapter.py` — the constraint is recorded where the next author will hit it.
