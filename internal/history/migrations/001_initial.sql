CREATE TABLE runs (
  id TEXT PRIMARY KEY NOT NULL,
  engine_run_id TEXT UNIQUE,
  spec_hash TEXT NOT NULL,
  spec_json TEXT NOT NULL CHECK(json_valid(spec_json) AND length(CAST(spec_json AS BLOB)) <= 4096),
  created_at TEXT NOT NULL,
  finished_at TEXT,
  status TEXT NOT NULL CHECK(status IN ('running','complete','failed','canceled')),
  report_json TEXT CHECK(report_json IS NULL OR (json_valid(report_json) AND length(CAST(report_json AS BLOB)) <= 1048576)),
  CHECK((status = 'running' AND finished_at IS NULL AND report_json IS NULL AND engine_run_id IS NULL)
     OR (status != 'running' AND finished_at IS NOT NULL AND report_json IS NOT NULL))
) STRICT;
CREATE INDEX runs_created ON runs(created_at DESC, id DESC);
