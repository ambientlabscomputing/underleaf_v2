import pytest

from e2e_testing.fixtures.edge.orchestrator import orchestrator
from e2e_testing.fixtures.edge.edge_build import edge_build
from e2e_testing.fixtures.config import test_config

pytest.mark.infra
def test_health_endpoint(test_config, orchestrator):
    """
    Test the health endpoint of the Orchestrator server
    """
    import requests
    response = requests.get(f"http://localhost:{test_config.ports.orchestrator_http_port}/api/v1/health")
    assert response.status_code == 200
    assert response.json() == {"status": "OK"}
