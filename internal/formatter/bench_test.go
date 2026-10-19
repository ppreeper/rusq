package formatter_test

import (
	"testing"

	"github.com/ppreeper/rusq/internal/formatter"
	"github.com/ppreeper/rusq/internal/settings"
)

var benchInput = []byte(`with recent as (
  select id, name, email from users where created_at > now() - interval '7 days'
)
select u.id, u.name, count(*) as n, sum(o.total) as total, avg(o.total) as avg_total
from users u
join orders o on o.user_id = u.id
left join items i on i.order_id = o.id
where u.active = true and (o.total > 100 or i.qty > 5)
group by u.id, u.name
having count(*) > 1
order by n desc, u.id
limit 10;
`)

func BenchmarkFormat(b *testing.B) {
	f := formatter.New(settings.Default())
	b.SetBytes(int64(len(benchInput)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = f.FormatBytes(benchInput)
	}
}

func BenchmarkFormatIdempotent(b *testing.B) {
	f := formatter.New(settings.Default())
	once := f.FormatBytes(benchInput)
	b.SetBytes(int64(len(once)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = f.FormatBytes(once)
	}
}
