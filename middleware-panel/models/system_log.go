package models

import "time"

type SystemLog struct {
	ID        uint      `gorm:"primaryKey;autoIncrement;column:id"`
	Level     string    `gorm:"column:level;type:text"`
	Message   string    `gorm:"column:message;type:text"`
	TraceID   string    `gorm:"column:trace_id;type:text"`
	Metadata  string    `gorm:"column:metadata;type:jsonb"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamp"`
}

func (SystemLog) TableName() string {
	return "system_logs"
}