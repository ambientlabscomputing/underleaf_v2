import pytest

from e2e_testing.fixtures.cloud.cloud_api import cloud_api
from e2e_testing.fixtures.config import test_config

pytest.mark.infra
def test_health_endpoint(test_config, cloud_api):
    """
    Test the health endpoint of the Cloud API server
    """
    import requests
    response = requests.get(f"http://localhost:{test_config.ports.cloud_api_http_port}/api/v2/cloud/health")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}
