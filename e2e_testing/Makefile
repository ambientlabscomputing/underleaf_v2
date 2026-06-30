install:
	pip install -e ".[dev]"

clean:
	find . -type d -name __pycache__ -exec rm -rf {} +
	find . -name "*.pyc" -delete

test: clean
	pytest .
