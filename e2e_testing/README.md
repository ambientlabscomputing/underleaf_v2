# E2E Testing

## Structure

- Fixtures: pytest fixtures for providing all needed dependencies
- Tests: actual tests following the tested app's structure as best as possible

e.g.,

- fixtures
    - servers
        - fooApp.py # starts the Foo app API server
- tests
    - fooApp.py
        - api
            - health
                test_health.py # hits the Foo /health/ API
