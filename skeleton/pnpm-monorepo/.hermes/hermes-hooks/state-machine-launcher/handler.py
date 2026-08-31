"""Spawn the state machine on Hermes gateway startup.

State machine is the built `dist/index.js` in packages/hermes-process,
launched with Node.js. {{PROJECT_ROOT}} is replaced by .hermes/bootstrap.sh.
"""
import logging
import os
import shutil
import subprocess
from pathlib import Path

logger = logging.getLogger("hooks.state-machine-launcher")

# Templated by bootstrap.sh
PROJECT_ROOT = "{{PROJECT_ROOT}}"
BOARD_SLUG = "{{BOARD_SLUG}}"
PROJECT_SLUG = "{{PROJECT_SLUG}}"

PID_FILE = Path.home() / ".hermes" / "state-machine.pid"
LOG_FILE = Path.home() / ".hermes" / "logs" / f"state-machine-{PROJECT_SLUG}.log"


def _is_running() -> bool:
    if not PID_FILE.exists():
        return False
    try:
        pid = int(PID_FILE.read_text().strip())
        os.kill(pid, 0)
        return True
    except (ValueError, ProcessLookupError, PermissionError):
        return False


async def handle(event_type: str, context: dict) -> None:
    if PROJECT_ROOT == "{{" + "PROJECT_ROOT}}":
        logger.warning("state-machine-launcher: PROJECT_ROOT not configured; skipping")
        return

    # hermes-process: либо workspace-пакет, либо установленная зависимость
    # (@foxford/hermes-process из registry — приезжает уже собранным).
    root = Path(PROJECT_ROOT)
    candidates = [
        root / "packages" / "hermes-process" / "build" / "state-machine" / "index.js",
        root / "packages" / "hermes-process" / "dist" / "index.js",
        root / "node_modules" / "@foxford" / "hermes-process" / "build" / "state-machine" / "index.js",
    ]
    built = next((p for p in candidates if p.is_file()), None)
    if built is None:
        logger.error(
            "state machine bundle not found (looked at: %s). "
            "Run .hermes/bootstrap.sh or `pnpm --filter @foxford/hermes-process build`.",
            ", ".join(str(p) for p in candidates),
        )
        return

    if _is_running():
        logger.info("state machine already running; skipping spawn")
        return

    node = shutil.which("node")
    if not node:
        logger.error("node not found in PATH")
        return

    LOG_FILE.parent.mkdir(parents=True, exist_ok=True)
    env = {
        **os.environ,
        "HERMES_BOARD": BOARD_SLUG,
        "HERMES_PROJECT_ROOT": PROJECT_ROOT,
        "HERMES_PROJECT_SLUG": PROJECT_SLUG,
    }

    logger.info("spawning state machine for %s via %s", PROJECT_SLUG, node)
    with open(LOG_FILE, "a") as fp:
        subprocess.Popen(
            [node, str(built)],
            cwd=str(process_pkg),
            env=env,
            stdout=fp,
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )
