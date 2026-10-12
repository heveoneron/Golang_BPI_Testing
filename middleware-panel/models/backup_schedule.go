package models

import "time"

type BackupSchedule struct {
	ID               int        `json:"id" db:"id"`
	DatabaseName     string     `json:"database_name" db:"database_name"`
	DatabaseType     string     `json:"database_type" db:"database_type"`
	BackupLevel      string     `json:"backup_level" db:"backup_level"`
	BackupLocation   string     `json:"backup_location" db:"backup_location"`
	ScheduleMode     string     `json:"schedule_mode" db:"schedule_mode"`
	ExecutionTime    string     `json:"execution_time" db:"execution_time"`
	WeeklyDays       string     `json:"weekly_days" db:"weekly_days"`
	MonthlyDays      string     `json:"monthly_days" db:"monthly_days"`
	CronExpression   string     `json:"cron_expression" db:"cron_expression"`
	RetentionDays    int        `json:"retention_days" db:"retention_days"`
	IsActive         bool       `json:"is_active" db:"is_active"`
	LastBackupTime   *time.Time `json:"last_backup_time" db:"last_backup_time"`
	LastBackupStatus *string    `json:"last_backup_status" db:"last_backup_status"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`
}

func (b *BackupSchedule) GetStatus() string {
	if b.LastBackupStatus == nil {
		return "PENDING"
	}
	return *b.LastBackupStatus
}
