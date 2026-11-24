-- Destination schema for the MySQL -> PostgreSQL scenario.
-- The DataX PostgreSQL writer inserts into this table; each run truncates it
-- through the job's preSql.

DROP TABLE IF EXISTS bench_dest;
CREATE TABLE bench_dest (
  id         BIGINT        NOT NULL PRIMARY KEY,
  name       VARCHAR(64)   NOT NULL,
  email      VARCHAR(96)   NOT NULL,
  age        INTEGER       NOT NULL,
  score      NUMERIC(10,2) NOT NULL,
  status     VARCHAR(16)   NOT NULL,
  city       VARCHAR(32)   NOT NULL,
  payload    VARCHAR(255)  NOT NULL,
  created_at TIMESTAMP(3)  NOT NULL,
  updated_at TIMESTAMP(3)  NOT NULL,
  flag       SMALLINT      NOT NULL,
  note       VARCHAR(128)  NULL
);
