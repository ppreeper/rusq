SELECT
	u.id,
	(
		SELECT COUNT(*)
		FROM orders AS o
		WHERE o.user_id = u.id
	) AS order_count
FROM users AS u
WHERE
	u.id IN (
		SELECT user_id
		FROM memberships
		WHERE level = 3
	);
