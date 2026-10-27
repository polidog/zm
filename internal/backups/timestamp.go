package backups

import "time"

// Timestamp はローカル時刻の YYYYMMDD-HHMMSS。
func Timestamp(t time.Time) string { return t.Format("20060102-150405") }
