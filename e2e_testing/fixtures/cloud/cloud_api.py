import time
import urllib.request
import pytest
import subprocess

from e2e_testing.fixtures.config import test_config

_STARTUP_TIMEOUT_S = 30

@pytest.fixture(scope="session")
def cloud_api(test_config):
    """
    cloud_api starts and kills the Cloud API server
    """
    process = subprocess.Popen(["make", "run"], cwd=test_config.repo_root_dir+"/cloud/cloud_api")

    health_url = f"http://localhost:{test_config.ports.cloud_api_http_port}/api/v2/cloud/health"
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
        raise RuntimeError(f"Cloud API did not become ready within {_STARTUP_TIMEOUT_S}s")

    yield
    process.terminate()
