from pydantic import BaseModel, Field
import pytest

class Ports(BaseModel):
    orchestrator_http_port: int = Field(default=9090, description="The port for the Orchestrator HTTP server")
    connections_node_port: int = Field(default=9021, description="The port for the Connections Node server")
    connections_gateway_port: int = Field(default=9020, description="The port for the Connections Gateway server")
    conn_worker_http_port: int = Field(default=9070, description="The port for the Conn Worker HTTP server")
    agent_http_port: int = Field(default=9091, description="The port for the Agent HTTP server")
    orchestrator_grpc_port: int = Field(default=50101, description="The port for the Orchestrator gRPC server")
    conn_worker_grpc_port: int = Field(default=50102, description="The port for the Conn Worker gRPC server")
    agent_grpc_port: int = Field(default=50103, description="The port for the Agent gRPC server")
    cloud_api_http_port: int = Field(default=8080, description="The port for the Cloud API HTTP server")

class TestConfig(BaseModel):
    repo_root_dir: str = Field(default="~/ambient_labs/underleaf_v2", description="The root directory of the repository")
    ports: Ports = Field(default_factory=Ports, description="The ports configuration for the services")

@pytest.fixture(scope="session")
def test_config():
    """
    test_config loads the test configuration from a YAML file
    """
    import yaml
    default_config = TestConfig()
    with open("test_config.yaml", "r") as f:
        config_data = yaml.safe_load(f)
    yield TestConfig(**{**default_config.model_dump(), **config_data})
    return
