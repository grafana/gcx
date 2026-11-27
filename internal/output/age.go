package output

import (
	"fmt"
	"time"
)

func RelativeAge(d time.Duration) string {
	later := d < 0
	if later {
		d = -d
	}
	var s string
	switch {
	case d < time.Minute:
		if later {
			return "in under a minute"
		}
		return "just now"
	case d < time.Hour:
		s = fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		s = fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		s = fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		s = fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	}
	if later {
		return "in " + s
	}
	return s + " ago"
}
