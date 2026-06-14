package utils

import "time"

func ParseDuration(durationStr string) time.Duration {
	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return 0
	}
	return duration
}
