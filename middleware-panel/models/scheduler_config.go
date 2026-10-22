package models

import "gorm.io/gorm"

type SchedulerConfig struct {
	gorm.Model
	IsActive           bool   `gorm:"default:false"`
	PullIsActive       bool   `gorm:"default:false"`
	Frequency          string `gorm:"type:varchar(20);default:'daily'"`
	StartDate          string `gorm:"type:varchar(15)"`
	StartTime          string `gorm:"type:varchar(10)"`
	RecurEvery         int    `gorm:"default:1"`
	IsRepeatEnabled    bool   `gorm:"default:false"`
	RepeatIntervalMins int    `gorm:"default:0"`

	WeeklyDays    string `gorm:"type:varchar(50)"`
	MonthlyMonths string `gorm:"type:varchar(100)"`
	MonthlyDays   string `gorm:"type:varchar(100)"`

	BatchLimit int `gorm:"default:1"` // Jumlah data ditarik per eksekusi (default: 1 untuk testing)

	Mode string `gorm:"type:varchar(20);default:'scheduler'"`
}
