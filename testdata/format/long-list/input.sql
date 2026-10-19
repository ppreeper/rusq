SELECT id, name, email, created_at, updated_at, status, score, rank FROM users WHERE active = TRUE ORDER BY score DESC;
