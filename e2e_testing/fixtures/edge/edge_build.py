import pytest
import subprocess
from e2e_testing.fixtures.config import test_config

@pytest.fixture(scope="session")
def edge_build(test_config):
    """builds the edge components"""
    subprocess.run(["make", "install"], check=True, cwd=test_config.repo_root_dir+"/edge")
