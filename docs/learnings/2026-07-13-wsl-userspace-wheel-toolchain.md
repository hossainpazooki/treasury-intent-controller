ts: 2026-07-13T22:37:24Z
commit: 6adff98
session: 71412dd0-24d9-4663-b2a9-0130c545ee1f (atlas-treasury-payment-loop)
status: verified

fact: The `ke-artifact-py` wheel builds and installs in this host's WSL Ubuntu entirely user-space — no sudo, despite the distro lacking pip/venv/rustc: rustup via `sh.rustup.rs -y --profile minimal`; maturin as the prebuilt `maturin-x86_64-unknown-linux-musl` binary into `~/.local/bin` (avoids a slow `cargo install`); pip via `get-pip.py --user --break-system-packages`; `CARGO_TARGET_DIR=$HOME/tic-target` keeps compilation off `/mnt/c` (release build 14s). This is the standing local Linux lane for the scorer's wheel tests, complementing the WSL Go `-race` lane.

basis: bootstrap output 2026-07-12: `rustc 1.85.0`, `maturin 1.14.1`, `pip 26.1.2`, `📦 Built wheel for abi3 Python ≥ 3.9 to /home/hossainpazooki/tic-wheels/ke_artifact_py-0.0.0-cp39-abi3-manylinux_2_34_x86_64.whl`, `WHEEL-IMPORT-OK`. Preconditions checked first: `sudo -n` → "a password is required"; `python3 -m venv` → ensurepip missing.

re-verify: `wsl -e bash -lc 'python3 -c "import ke_artifact_py; print(ke_artifact_py.__doc__[:30])"'`
