package services

import (
	"middleware-panel/models"
	"middleware-panel/repository"
	"strconv"
	"strings"
)

type SchedulerService interface {
	GetConfig() (*models.SchedulerConfig, error)
	UpdateConfigFromForm(form map[string]interface{}) (*models.SchedulerConfig, error)
}

type schedulerService struct {
	repo repository.SchedulerRepository
}

func NewSchedulerService(repo repository.SchedulerRepository) SchedulerService {
	return &schedulerService{repo}
}

func (s *schedulerService) GetConfig() (*models.SchedulerConfig, error) {
	return s.repo.GetFirstConfig()
}

func (s *schedulerService) UpdateConfigFromForm(form map[string]interface{}) (*models.SchedulerConfig, error) {
	schedConfig, err := s.repo.GetFirstConfig()
	if err != nil {
		schedConfig = &models.SchedulerConfig{} // Buat baru jika belum ada
	}

	schedConfig.IsActive = (form["is_active"] == "on")

	if val, ok := form["frequency"].(string); ok {
		schedConfig.Frequency = val
		// Set mode otomatis berdasarkan frequency
		if val == "realtime" {
			schedConfig.Mode = "realtime"
		} else {
			schedConfig.Mode = "scheduler"
		}
	}

	if val, ok := form["start_date"].(string); ok {
		schedConfig.StartDate = val
	}
	if val, ok := form["start_time"].(string); ok {
		schedConfig.StartTime = val
	}

	if recurStr, ok := form["recur_every"].(string); ok {
		recur, _ := strconv.Atoi(recurStr)
		if recur < 1 {
			recur = 1
		}
		schedConfig.RecurEvery = recur
	}

	if weeklyDays, ok := form["weekly_days"].([]string); ok {
		schedConfig.WeeklyDays = strings.Join(weeklyDays, ",")
	}
	if monthlyMonths, ok := form["monthly_months"].([]string); ok {
		schedConfig.MonthlyMonths = strings.Join(monthlyMonths, ",")
	}
	if monthlyDays, ok := form["monthly_days"].([]string); ok {
		schedConfig.MonthlyDays = strings.Join(monthlyDays, ",")
	}

	schedConfig.IsRepeatEnabled = (form["is_repeat_enabled"] == "on")
	if schedConfig.IsRepeatEnabled {
		if repeatStr, ok := form["repeat_interval_mins"].(string); ok {
			schedConfig.RepeatIntervalMins, _ = strconv.Atoi(repeatStr)
		}
	} else {
		schedConfig.RepeatIntervalMins = 0
	}

	err = s.repo.UpdateConfig(schedConfig)
	return schedConfig, err
}
