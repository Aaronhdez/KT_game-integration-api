-- Run by the postgres image only when its data volume is empty (first start or
-- after `docker compose down -v`), as POSTGRES_USER, who becomes the owner.
-- The integration tests use this database (TEST_DATABASE_URL), never the
-- service's own one.
CREATE DATABASE gameapi_test;
