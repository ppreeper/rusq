SELECT id, name, email
FROM users
WHERE active = TRUE AND score > 10
ORDER BY name;
