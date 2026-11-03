# D001 results

10,000,000 events in one project-day, median of 3 cold runs, 25 ms added to each S3 request. ClickHouse 25.8.33.6.

| layout | files in layout | size |
|---|---|---|
| day | 1 | 237.4 MB |
| hour | 24 | 239.2 MB |
| 5min | 288 | 260.1 MB |

## S3 (engine reads the objects)

| layout | query | files | engine | wall s | requests | MB read | peak MB | correct |
|---|---|---|---|---|---|---|---|---|
| day | q1_needle | 1 | clickhouse | 0.322 | 10 | 15.063 | 72.1 | yes |
| day | q1_needle | 1 | duckdb | 1.244 | 12 | 7.462 | 89.2 | yes |
| day | q1_needle | 1 | datafusion | 0.379 | 8 | 8.202 | 136.1 | yes |
| day | q2_attr | 1 | clickhouse | 1.948 | 57 | 243.186 | 553.9 | yes |
| day | q2_attr | 1 | duckdb | 2.387 | 202 | 166.182 | 715.0 | yes |
| day | q2_attr | 1 | datafusion | 0.696 | 191 | 118.355 | 390.9 | yes |
| day | q3_levels | 1 | clickhouse | 0.594 | 57 | 151.493 | 205.1 | yes |
| day | q3_levels | 1 | duckdb | 2.289 | 102 | 3.571 | 86.2 | yes |
| day | q3_levels | 1 | datafusion | 0.774 | 103 | 3.42 | 164.3 | yes |
| day | q4_page | 1 | clickhouse | 0.28 | 10 | 12.41 | 39.3 | yes |
| day | q4_page | 1 | duckdb | 0.211 | 7 | 2.665 | 86.5 | yes |
| day | q4_page | 1 | datafusion | 0.312 | 6 | 1.955 | 117.8 | yes |
| hour | q1_needle | 1 | clickhouse | 0.287 | 9 | 13.058 | 43.7 | yes |
| hour | q1_needle | 1 | duckdb | 1.275 | 12 | 6.053 | 87.2 | yes |
| hour | q1_needle | 1 | datafusion | 0.278 | 8 | 6.945 | 167.0 | yes |
| hour | q2_attr | 24 | clickhouse | 0.927 | 147 | 335.714 | 283.4 | yes |
| hour | q2_attr | 24 | duckdb | 3.647 | 287 | 129.879 | 614.4 | yes |
| hour | q2_attr | 24 | datafusion | 1.97 | 288 | 144.115 | 501.1 | yes |
| hour | q3_levels | 24 | clickhouse | 0.81 | 147 | 228.25 | 164.7 | yes |
| hour | q3_levels | 24 | duckdb | 2.944 | 288 | 4.357 | 93.9 | yes |
| hour | q3_levels | 24 | datafusion | 1.807 | 192 | 15.481 | 234.9 | yes |
| hour | q4_page | 1 | clickhouse | 0.278 | 9 | 10.117 | 25.2 | yes |
| hour | q4_page | 1 | duckdb | 0.245 | 7 | 2.035 | 94.1 | yes |
| hour | q4_page | 1 | datafusion | 0.269 | 8 | 2.517 | 130.2 | yes |
| 5min | q1_needle | 12 | clickhouse | 0.164 | 26 | 11.736 | 105.1 | yes |
| 5min | q1_needle | 12 | duckdb | 1.257 | 24 | 10.834 | 108.6 | yes |
| 5min | q1_needle | 12 | datafusion | 0.969 | 48 | 13.249 | 267.6 | yes |
| 5min | q2_attr | 288 | clickhouse | 1.002 | 578 | 260.942 | 136.3 | yes |
| 5min | q2_attr | 288 | duckdb | 3.493 | 576 | 260.051 | 690.9 | yes |
| 5min | q2_attr | 288 | datafusion | 17.87 | 1152 | 409.846 | 919.9 | yes |
| 5min | q3_levels | 288 | clickhouse | 0.76 | 578 | 260.942 | 184.4 | yes |
| 5min | q3_levels | 288 | duckdb | 2.652 | 576 | 260.051 | 347.4 | yes |
| 5min | q3_levels | 288 | datafusion | 17.471 | 1152 | 153.914 | 693.0 | yes |
| 5min | q4_page | 12 | clickhouse | 0.143 | 26 | 11.736 | 29.9 | yes |
| 5min | q4_page | 12 | duckdb | 1.261 | 24 | 10.834 | 123.3 | yes |
| 5min | q4_page | 12 | datafusion | 0.906 | 48 | 8.45 | 224.4 | yes |

## SFTP (download to cache, then query locally)

| layout | query | files | MB downloaded | download s | engine | query s | total s | peak MB | correct |
|---|---|---|---|---|---|---|---|---|---|
| day | q1_needle | 1 | 237.4 | 2.079 | clickhouse | 0.062 | 2.141 | 80.5 | yes |
| day | q1_needle | 1 | 237.4 | 2.079 | duckdb | 0.137 | 2.216 | 81.8 | yes |
| day | q1_needle | 1 | 237.4 | 2.079 | datafusion | 0.202 | 2.281 | 124.4 | yes |
| day | q2_attr | 1 | 237.4 | 2.154 | clickhouse | 0.503 | 2.657 | 376.8 | yes |
| day | q2_attr | 1 | 237.4 | 2.154 | duckdb | 0.465 | 2.619 | 624.1 | yes |
| day | q2_attr | 1 | 237.4 | 2.154 | datafusion | 0.329 | 2.483 | 454.8 | yes |
| day | q3_levels | 1 | 237.4 | 2.017 | clickhouse | 0.1 | 2.117 | 235.7 | yes |
| day | q3_levels | 1 | 237.4 | 2.017 | duckdb | 0.1 | 2.117 | 86.2 | yes |
| day | q3_levels | 1 | 237.4 | 2.017 | datafusion | 0.18 | 2.197 | 178.4 | yes |
| day | q4_page | 1 | 237.4 | 2.016 | clickhouse | 0.03 | 2.046 | 57.1 | yes |
| day | q4_page | 1 | 237.4 | 2.016 | duckdb | 0.099 | 2.115 | 87.2 | yes |
| day | q4_page | 1 | 237.4 | 2.016 | datafusion | 0.16 | 2.176 | 106.3 | yes |
| hour | q1_needle | 1 | 10.0 | 0.116 | clickhouse | 0.039 | 0.155 | 77.1 | yes |
| hour | q1_needle | 1 | 10.0 | 0.116 | duckdb | 0.115 | 0.231 | 87.2 | yes |
| hour | q1_needle | 1 | 10.0 | 0.116 | datafusion | 0.154 | 0.27 | 150.7 | yes |
| hour | q2_attr | 24 | 239.2 | 2.868 | clickhouse | 0.532 | 3.4 | 365.1 | yes |
| hour | q2_attr | 24 | 239.2 | 2.868 | duckdb | 0.466 | 3.334 | 582.9 | yes |
| hour | q2_attr | 24 | 239.2 | 2.868 | datafusion | 0.369 | 3.237 | 524.0 | yes |
| hour | q3_levels | 24 | 239.2 | 2.911 | clickhouse | 0.106 | 3.017 | 229.7 | yes |
| hour | q3_levels | 24 | 239.2 | 2.911 | duckdb | 0.116 | 3.027 | 94.1 | yes |
| hour | q3_levels | 24 | 239.2 | 2.911 | datafusion | 0.246 | 3.157 | 247.6 | yes |
| hour | q4_page | 1 | 10.0 | 0.123 | clickhouse | 0.024 | 0.147 | 48.6 | yes |
| hour | q4_page | 1 | 10.0 | 0.123 | duckdb | 0.111 | 0.234 | 94.1 | yes |
| hour | q4_page | 1 | 10.0 | 0.123 | datafusion | 0.154 | 0.277 | 128.9 | yes |
| 5min | q1_needle | 12 | 10.8 | 0.579 | clickhouse | 0.034 | 0.613 | 112.4 | yes |
| 5min | q1_needle | 12 | 10.8 | 0.579 | duckdb | 0.105 | 0.684 | 94.2 | yes |
| 5min | q1_needle | 12 | 10.8 | 0.579 | datafusion | 0.173 | 0.752 | 249.1 | yes |
| 5min | q2_attr | 288 | 260.1 | 14.014 | clickhouse | 0.604 | 14.618 | 166.7 | yes |
| 5min | q2_attr | 288 | 260.1 | 14.014 | duckdb | 0.506 | 14.52 | 449.7 | yes |
| 5min | q2_attr | 288 | 260.1 | 14.014 | datafusion | 0.909 | 14.923 | 793.9 | yes |
| 5min | q3_levels | 288 | 260.1 | 14.061 | clickhouse | 0.1 | 14.161 | 183.1 | yes |
| 5min | q3_levels | 288 | 260.1 | 14.061 | duckdb | 0.2 | 14.261 | 123.3 | yes |
| 5min | q3_levels | 288 | 260.1 | 14.061 | datafusion | 0.932 | 14.993 | 748.6 | yes |
| 5min | q4_page | 12 | 10.8 | 0.583 | clickhouse | 0.017 | 0.6 | 49.3 | yes |
| 5min | q4_page | 12 | 10.8 | 0.583 | duckdb | 0.092 | 0.675 | 123.3 | yes |
| 5min | q4_page | 12 | 10.8 | 0.583 | datafusion | 0.147 | 0.73 | 216.6 | yes |

Peak MB: ClickHouse `system.query_log.memory_usage`; DuckDB and DataFusion process peak RSS minus the RSS of a process that only imports that engine.

## Damaged segments (local file, `count(*)` for the project)

| case | ClickHouse | server still up | DuckDB | DataFusion |
|---|---|---|---|---|
| truncated | error: The table structure cannot be extracted from a Parquet format file. Error: std::exception. Code: 1001, type: parquet::ParquetInvalidOrCorruptedFileException, e.what() = Invalid: Parquet magic bytes no | True | error: _duckdb.InvalidInputException: Invalid Input Error: No magic bytes found at end of file '/cache/0193a000-0000-7000-8000-000000000002/20261101/bad/truncated.parquet' | error: Exception: DataFusion error: Parquet error: Parquet error: Invalid Parquet file. Corrupt footer |
| garbage | error: The table structure cannot be extracted from a Parquet format file. Error: std::exception. Code: 1001, type: parquet::ParquetInvalidOrCorruptedFileException, e.what() = Invalid: Parquet magic bytes no | True | error: _duckdb.InvalidInputException: Invalid Input Error: No magic bytes found at end of file '/cache/0193a000-0000-7000-8000-000000000002/20261101/bad/garbage.parquet' | error: Exception: DataFusion error: Parquet error: Parquet error: Invalid Parquet file. Corrupt footer |
| footer_lies | error: The table structure cannot be extracted from a Parquet format file. Error: std::exception. Code: 1001, type: parquet::ParquetInvalidOrCorruptedFileException, e.what() = Invalid: Parquet file size is 9 | True | error: _duckdb.InvalidInputException: Invalid Input Error: Footer length error in file '/cache/0193a000-0000-7000-8000-000000000002/20261101/bad/footer_lies.parquet' | error: pyo3_runtime.PanicException: range start must not be greater than end: 18446744073670266028 <= 524280 |
| empty | error: The table structure cannot be extracted from a Parquet format file. Error: The table structure cannot be extracted from a Parquet format file: the file is empty. You can specify the structure manually | True | error: _duckdb.InvalidInputException: Invalid Input Error: File '/cache/0193a000-0000-7000-8000-000000000002/20261101/bad/empty.parquet' too small to be a Parquet file | error: ValueError: Schema error: No field named project_id. |
| other_project | rows: [(0,)] | True | rows: [(0,)] | rows: [{'count(*)': 0}] |
