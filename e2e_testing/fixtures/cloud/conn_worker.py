import pytest
import subprocess

import urllib.request
import time

from e2e_testing.fixtures.cloud.cloud_api import _STARTUP_TIMEOUT_S
from e2e_testing.fixtures.config import test_config

@pytest.fixture(scope="session")
def conn_worker(test_config):
    """
    conn_worker starts and kills the Conn Worker server
    """
    process = subprocess.Popen(["make", "run"], cwd=test_config.repo_root_dir+"/cloud/conn_worker")

    health_url = f"http://localhost:{test_config.ports.conn_worker_http_port}/api/v2/connections/health"
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
        raise RuntimeError(f"Conn Worker did not become ready within {_STARTUP_TIMEOUT_S}s")

    yield
    process.terminate()