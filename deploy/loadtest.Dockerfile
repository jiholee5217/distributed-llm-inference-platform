FROM python:3.13-slim
WORKDIR /app
COPY worker/requirements.txt worker/requirements-dev.txt worker/
RUN pip install --no-cache-dir -r worker/requirements-dev.txt
COPY loadtest loadtest
ENTRYPOINT ["locust", "-f", "/app/loadtest/locustfile.py"]
