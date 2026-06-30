import pytest

from e2e_testing.fixtures.cloud.conn_worker import conn_worker
from e2e_testing.fixtures.config import test_config

def test_health_endpoint(test_config, conn_worker):
    """
    Test the health endpoint of the Conn Worker server
    """
    import requests
    response = requests.get(f"http://localhost:{test_config.ports.conn_worker_http_port}/api/v2/connections/health")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}
