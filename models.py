"""Role -> model resolution from config/models.json.

Model choice lives in exactly one place (the config); code never carries a
model literal. The mapping is deliberately flat (role -> model): three roles
in one service do not earn rigor's tier->model indirection. Fail-loud
posture matches context.py's doc loading: a missing/malformed config or an
unknown role raises immediately.
"""
from __future__ import annotations

import json
from pathlib import Path

CONFIG_PATH = Path(__file__).resolve().parent / "config" / "models.json"


def load_config(path: Path | None = None) -> dict[str, str]:
    path = path if path is not None else CONFIG_PATH
    if not path.is_file():
        raise FileNotFoundError(f"model config not found: {path}")
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict) or not data:
        raise ValueError(f"model config must be a non-empty JSON object: {path}")
    for role, model in data.items():
        if not isinstance(model, str) or not model:
            raise ValueError(f"role {role!r} must map to a model id string: {path}")
    return data


def resolve(role: str, path: Path | None = None) -> str:
    config = load_config(path)
    if role not in config:
        raise KeyError(f"unknown model role {role!r}; config has {sorted(config)}")
    return config[role]
