"""The conformance suite must not know what language the server is written in.

tests/contract/ is the executable definition of Analog. It is only worth that if it
reaches the server the way any other client does — over HTTP, to a separate process.
One `from analog...` import and the suite silently stops being portable, so the rule
is asserted rather than trusted.
"""

from __future__ import annotations

import ast
from pathlib import Path

import pytest

pytestmark = pytest.mark.contract

CONTRACT_DIR = Path(__file__).resolve().parent


def _imported_modules(path: Path) -> set[str]:
    tree = ast.parse(path.read_text(), filename=str(path))
    names: set[str] = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            names.update(alias.name for alias in node.names)
        elif isinstance(node, ast.ImportFrom) and node.module and node.level == 0:
            names.add(node.module)
    return names


@pytest.mark.parametrize("path", sorted(CONTRACT_DIR.glob("test_*.py")), ids=lambda p: p.name)
def test_no_contract_test_imports_the_implementation(path):
    offenders = {m for m in _imported_modules(path)
                 if m == "analog" or m.startswith("analog.")}
    assert not offenders, (
        f"{path.name} imports {sorted(offenders)}. The contract suite talks to a "
        f"server over HTTP; anything that needs the Python objects belongs in "
        f"tests/unit/, which is rewritten in the server's language rather than run "
        f"against a binary.")
