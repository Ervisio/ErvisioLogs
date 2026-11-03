FROM python:3.12-slim
WORKDIR /bench
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
# DuckDB fetches httpfs from extensions.duckdb.org at run time unless it is installed ahead.
RUN python -c "import duckdb; duckdb.connect().execute('INSTALL httpfs')"
COPY bench/ .
