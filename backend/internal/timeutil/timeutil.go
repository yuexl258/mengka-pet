package timeutil

import "time"

var Beijing = time.FixedZone("Asia/Shanghai", 8*60*60)

func Now() string {
	return time.Now().In(Beijing).Format(time.RFC3339)
}

func Current() time.Time {
	return time.Now().In(Beijing)
}
