WITH recent AS (
    SELECT
        o.user_id,
        COUNT(*) AS n,
        SUM(o.total) AS revenue
    FROM orders AS o
    WHERE o.created_at > CURRENT_TIMESTAMP
    GROUP BY o.user_id
    HAVING COUNT(*) > 1
)
SELECT
    u.id,
    u.name,
    r.n,
    r.revenue,
    CASE WHEN r.n > 10 THEN 'high' ELSE 'low' END AS bucket
FROM users AS u
JOIN recent AS r ON r.user_id = u.id
LEFT JOIN addresses AS a ON a.user_id = u.id
WHERE u.active = TRUE
ORDER BY r.revenue DESC
LIMIT 100;
