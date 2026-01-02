FROM python:3.11-slim

WORKDIR /app

RUN pip install --no-cache-dir temporalio

COPY worker.py .

CMD ["python", "-u", "worker.py"]
