package settings

import "testing"

func TestParseSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"", 0, true},
		{"0", 0, true},
		{"unlimited", 0, true},
		{"off", 0, true},
		{"1024", 1024, true},
		{"512MB", 512_000_000, true},
		{"512mb", 512_000_000, true},
		{"2GiB", 2 << 30, true},
		{"1k", 1 << 10, true},
		{"3gb", 3_000_000_000, true},
		{" 250 MiB ", 250 << 20, true},
		{"lots", 0, false},
		{"-5MB", 0, false},
		{"MB", 0, false},
	}
	for _, c := range cases {
		got, ok := ParseSize(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("ParseSize(%q) = (%d, %v), want (%d, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestDefaultCacheMaxSize(t *testing.T) {
	if s := Default(); s.CacheMaxSize != DefaultCacheMaxSize {
		t.Errorf("Default().CacheMaxSize = %d, want %d", s.CacheMaxSize, DefaultCacheMaxSize)
	}
}
