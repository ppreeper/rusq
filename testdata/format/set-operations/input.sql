SELECT id FROM users WHERE active = TRUE UNION SELECT id FROM admins WHERE active = TRUE ORDER BY id;
