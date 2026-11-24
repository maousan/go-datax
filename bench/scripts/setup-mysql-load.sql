-- Load the deterministic 1,000,000-row dataset from the CSV copied to
-- /tmp/go-datax-bench-rows.txt by setup-containers.sh.
-- \N in the file is interpreted as SQL NULL by LOAD DATA (default ESCAPED BY).

SET SESSION sql_log_bin = 0;

TRUNCATE TABLE bench_source;
LOAD DATA LOCAL INFILE '/tmp/go-datax-bench-rows.txt'
INTO TABLE bench_source
FIELDS TERMINATED BY ','
LINES TERMINATED BY '\n'
(id, name, email, age, score, status, city, payload,
 created_at, updated_at, flag, note);

ANALYZE TABLE bench_source;
ANALYZE TABLE bench_dest;
