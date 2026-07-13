"""Boot-time env configuration (`python -m tis`): the all-or-nothing resolver
config and the StaticFactSource override. These are the fail-loud seams — a
server configured to verify must never silently not-verify — tested without
starting a server."""

import pytest

from tis.__main__ import facts_from_env, resolver_from_env

RESOLVER_VARS = ("TIS_ARTIFACT_DIR", "TIS_ATLAS_INPUTS_DIR", "TIS_EXPORTED_AT_UNIX")


def _clear(monkeypatch):
    for var in RESOLVER_VARS + ("TIS_FACTS_JSON",):
        monkeypatch.delenv(var, raising=False)


def test_no_env_means_null_resolver_default(monkeypatch):
    _clear(monkeypatch)
    assert resolver_from_env() is None


@pytest.mark.parametrize("present", RESOLVER_VARS)
def test_partial_resolver_config_refuses_to_boot(monkeypatch, present):
    _clear(monkeypatch)
    monkeypatch.setenv(present, "some-value")
    with pytest.raises(SystemExit):
        resolver_from_env()


def test_no_facts_env_keeps_demo_default(monkeypatch):
    _clear(monkeypatch)
    assert facts_from_env() is None


def test_facts_env_builds_static_source(monkeypatch):
    _clear(monkeypatch)
    monkeypatch.setenv("TIS_FACTS_JSON", '{"amount_under_ceiling": 2000000}')
    facts = facts_from_env()
    assert facts is not None
    assert facts.get("amount_under_ceiling", "any-intent") == 2000000.0
    assert facts.get("unknown", "any-intent") is None


def test_facts_env_non_object_refuses(monkeypatch):
    _clear(monkeypatch)
    monkeypatch.setenv("TIS_FACTS_JSON", "[1, 2]")
    with pytest.raises(SystemExit):
        facts_from_env()
