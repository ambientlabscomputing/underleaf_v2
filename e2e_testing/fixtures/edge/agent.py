import time
import urllib.request
import pytest
import subprocess

from e2e_testing.fixtures.cloud.cloud_api import _STARTUP_TIMEOUT_S
from e2e_testing.fixtures.config import test_config
from e2e_testing.fixtures.edge.edge_build import edge_build


@pytest.fixture(scope="session")
def agent(edge_build, test_config):
    """
    agent starts and kills the edge agent daemon
    """
    process = subprocess.Popen(["ufagentd", "run"])

    health_url = f"http://localhost:{test_config.ports.agent_http_port}/api/v1/health"
    deadline = time.monotonic() + _STARTUP_TIMEOUT_S
    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen(health_url, timeout=1) as resp:
                if resp.status == 200:
                    break
        except Exception:
            time.sleep(0.5)
    else:
        process.terminate()
        raise RuntimeError(f"Agent did not become ready within {_STARTUP_TIMEOUT_S}s")

    yield
    process.terminate()
