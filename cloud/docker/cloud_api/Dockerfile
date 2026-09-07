FROM python:3.13-slim

WORKDIR /app

COPY ./cloud/cloud_api/src .
COPY ./cloud/cloud_api/pyproject.toml .

RUN apt-get update && apt-get install -y --no-install-recommends libpq-dev build-essential && rm -rf /var/lib/apt/lists/*

RUN pip install .

ENV PYTHONPATH=src
CMD ["python", "-m", "cloud_api.main"]
