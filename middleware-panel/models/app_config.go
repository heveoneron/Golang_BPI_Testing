package models

import "time"

type Config struct {
	Key         string    `gorm:"primaryKey;column:key;type:varchar(100)" json:"key"`
	Value       string    `gorm:"column:value;type:text" json:"value"`
	Description string    `gorm:"column:description;type:text" json:"description"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (Config) TableName() string {
	return "config"
}