package status

import (
	"testing"
	"time"
)

func TestDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		42 * time.Minute:             "42 мин",
		5*time.Hour + 12*time.Minute: "5 ч 12 мин",
		24 * time.Hour:               "1 день 0 ч",
		3*24*time.Hour + 4*time.Hour: "3 дня 4 ч",
		11 * 24 * time.Hour:          "11 дней 0 ч",
		22 * 24 * time.Hour:          "22 дня 0 ч",
		25 * 24 * time.Hour:          "25 дней 0 ч",
	} {
		if got := Duration(d); got != want {
			t.Errorf("%v: %q, ждали %q", d, got, want)
		}
	}
}
