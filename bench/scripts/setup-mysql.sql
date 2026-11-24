-- Schema for the DataX reference baseline.
--
-- The 1,000,000-row dataset is generated deterministically by
-- bench/tools/genrows and loaded with LOAD DATA LOCAL INFILE by
-- bench/scripts/setup-containers.sh. Loading the CSV is roughly two orders of
-- magnitude faster than a 1M-row INSERT ... SELECT with a recursive join.
--
-- Row shape (~128 bytes/row before InnoDB overhead), 12 mixed-type columns:
--   id BIGINT, name VARCHAR, email VARCHAR, age INT, score DECIMAL,
--   status VARCHAR, city VARCHAR, payload VARCHAR, created_at DATETIME(3),
--   updated_at DATETIME(3), flag TINYINT, note VARCHAR NULL

DROP TABLE IF EXISTS bench_source;
CREATE TABLE bench_source (
  id         BIGINT        NOT NULL PRIMARY KEY,
  name       VARCHAR(64)   NOT NULL,
  email      VARCHAR(96)   NOT NULL,
  age        INT           NOT NULL,
  score      DECIMAL(10,2) NOT NULL,
  status     VARCHAR(16)   NOT NULL,
  city       VARCHAR(32)   NOT NULL,
  payload    VARCHAR(255)  NOT NULL,
  created_at DATETIME(3)   NOT NULL,
  updated_at DATETIME(3)   NOT NULL,
  flag       TINYINT       NOT NULL,
  note       VARCHAR(128)  NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Destination mirrors the source schema; the writer truncates it per run.
DROP TABLE IF EXISTS bench_dest;
CREATE TABLE bench_dest LIKE bench_source;
