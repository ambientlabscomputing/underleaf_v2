# Fixtures are registered by importing them here; ruff would otherwise strip
# them from the test module as unused imports.
from e2e_testing.fixtures.config import test_config  # noqa: F401
from e2e_testing.fixtures.golden_path.stack import (  # noqa: F401
    base_url,
    browser_context_args,
    stack,
    stripe_catalog,
)
