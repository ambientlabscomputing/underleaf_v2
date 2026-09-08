FROM python:3.13-slim

WORKDIR /app

COPY ./cloud/cloud_api/src .
COPY ./cloud/cloud_api/pyproject.toml .

RUN apt-get update && apt-get install -y --no-install-recommends libpq-dev build-essential && rm -rf /var/lib/apt/lists/*

RUN pip install .

COPY ./cloud/cloud_api/alembic.ini .
COPY ./cloud/cloud_api/alembic ./alembic

ENV PYTHONPATH=src
CMD ["sh", "-c", "alembic upgrade head && python -m cloud_api.main"]
