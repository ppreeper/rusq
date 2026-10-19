SELECT * FROM t CONNECT BY PRIOR id = parent_id;
