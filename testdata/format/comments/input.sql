-- load the active users
SELECT id, name -- only what we need
FROM users
WHERE active = TRUE; -- filter
