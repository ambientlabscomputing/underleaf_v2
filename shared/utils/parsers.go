package utils

import "time"

func ParseDuration(durationStr string) time.Duration {
	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return 0
	}
	return duration
}

func ParseStringPtr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func StringPtr(s string) *string {
	return &s
}

func Int64PtrToInt(i *int64) int {
	if i == nil {
		return 0
	}
	return int(*i)
}

func IntToInt64Ptr(i int) *int64 {
	i64 := int64(i)
	return &i64
}
