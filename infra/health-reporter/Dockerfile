FROM python:3.13-slim
RUN pip install --no-cache-dir "psycopg[binary]==3.2.10" && useradd --uid 10001 reporter
COPY infra/health-reporter/reporter.py /app/reporter.py
USER 10001
ENTRYPOINT ["python","/app/reporter.py"]
