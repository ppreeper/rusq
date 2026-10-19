SELECT * FROM t LATERAL VIEW explode(arr) e AS x;
