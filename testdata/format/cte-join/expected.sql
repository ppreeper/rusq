WITH recent AS (
    SELECT o.user_id, COUNT(*) AS n
    FROM orders AS o
    WHERE o.created_at > CURRENT_TIMESTAMP
    GROUP BY o.user_id
)
SELECT u.id, u.name, r.n
FROM users AS u
JOIN recent AS r ON r.user_id = u.id
LEFT JOIN addresses AS a ON a.user_id = u.id
WHERE u.active = TRUE
ORDER BY r.n DESC
LIMIT 100;
