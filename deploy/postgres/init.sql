-- Local database setup. Postgres runs this once, when its data volume is
-- first created. On AWS, Terraform does the equivalent.
--
-- Each service gets its own schema and its own login, and can only use its
-- own schema. Its search_path points at that schema, so its migrations and
-- queries use unqualified table names.
-- Passwords are for this local database only.

CREATE ROLE event_api LOGIN PASSWORD 'event_api';
CREATE SCHEMA event AUTHORIZATION event_api;
ALTER ROLE event_api SET search_path = event;

CREATE ROLE booking_api LOGIN PASSWORD 'booking_api';
CREATE SCHEMA booking AUTHORIZATION booking_api;
ALTER ROLE booking_api SET search_path = booking;

-- No service may create objects outside its own schema.
REVOKE ALL ON SCHEMA public FROM PUBLIC;
