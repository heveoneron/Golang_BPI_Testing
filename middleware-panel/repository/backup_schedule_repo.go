package repository

import (
	"middleware-panel/models"

	"gorm.io/gorm"
)

type BackupScheduleRepository interface {
	GetAll() ([]models.BackupSchedule, error)
	GetByID(id int) (*models.BackupSchedule, error) // Tambahan Baru
	Update(schedule *models.BackupSchedule) error
}

type backupScheduleRepo struct {
	db *gorm.DB
}

func NewBackupScheduleRepository(db *gorm.DB) BackupScheduleRepository {
	return &backupScheduleRepo{db}
}

func (r *backupScheduleRepo) GetAll() ([]models.BackupSchedule, error) {
	var schedules []models.BackupSchedule
	err := r.db.Order("id asc").Find(&schedules).Error
	if err != nil {
		return nil, err
	}
	return schedules, nil
}

// Tambahan Fungsi Baru
func (r *backupScheduleRepo) GetByID(id int) (*models.BackupSchedule, error) {
	var schedule models.BackupSchedule
	err := r.db.First(&schedule, id).Error
	if err != nil {
		return nil, err
	}
	return &schedule, nil
}

func (r *backupScheduleRepo) Update(s *models.BackupSchedule) error {
	// Buat map update yang mengecualikan status backup
	updateData := map[string]interface{}{
		"database_name":   s.DatabaseName,
		"database_type":   s.DatabaseType,
		"backup_level":    s.BackupLevel,
		"backup_location": s.BackupLocation,
		
		"schedule_mode":   s.ScheduleMode,
		"execution_time":  s.ExecutionTime,
		"weekly_days":     s.WeeklyDays,
		"monthly_days":    s.MonthlyDays,
		"cron_expression": s.CronExpression,
		"retention_days":  s.RetentionDays,
		"is_active":       s.IsActive,
	}

	if s.LastBackupTime != nil {
		updateData["last_backup_time"] = s.LastBackupTime
		updateData["last_backup_status"] = s.LastBackupStatus
	}

	err := r.db.Model(&models.BackupSchedule{}).Where("id = ?", s.ID).Updates(updateData).Error

	return err
}
