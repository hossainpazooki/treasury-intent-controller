import json
import re
from pathlib import Path

import pytest

import models

ROOT = Path(__file__).resolve().parent
RESOLVE_RE = re.compile(r'models\.resolve\(\s*"([a-z_]+)"')


def roles_referenced_in_code() -> set[str]:
    found: set[str] = set()
    for py in ROOT.glob("*.py"):
        if py.name.startswith("test_") or py.name == "models.py":
            continue
        found |= set(RESOLVE_RE.findall(py.read_text(encoding="utf-8")))
    return found


def test_role_sync():
    # check-tier-sync analog: bidirectional, no exceptions list.
    code_roles = roles_referenced_in_code()
    config_roles = set(models.load_config())
    assert code_roles, "no models.resolve(...) call found in code"
    missing = code_roles - config_roles
    orphans = config_roles - code_roles
    assert not missing, f"roles used in code but absent from config: {sorted(missing)}"
    assert not orphans, f"orphan roles in config no code dispatches: {sorted(orphans)}"


def test_resolve_discussion():
    assert models.resolve("discussion") == "claude-opus-4-8"


def test_unknown_role_fails_loudly():
    with pytest.raises(KeyError) as exc:
        models.resolve("nonexistent-role")
    assert "nonexistent-role" in str(exc.value)


def test_missing_config_fails_loudly(tmp_path):
    with pytest.raises(FileNotFoundError):
        models.resolve("discussion", tmp_path / "absent.json")


def test_malformed_config_fails_loudly(tmp_path):
    bad = tmp_path / "models.json"
    bad.write_text(json.dumps({"discussion": 7}), encoding="utf-8")
    with pytest.raises(ValueError):
        models.resolve("discussion", bad)


def test_config_is_flat_role_to_model():
    config = models.load_config()
    assert all(isinstance(v, str) and v for v in config.values())
