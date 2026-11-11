package models

import "time"

// VendorMaster merepresentasikan Master Data Vendor Maximo & SAP S/4HANA
// sesuai spesifikasi Outbound Master Data Vendor (CIP/Non-CIP, Email, Bank Key, Bank Account, Account Holder)
type VendorMaster struct {
	VendorCode    string    `gorm:"primaryKey;column:vendor_code;type:varchar(50)" json:"vendor_code"`
	CIPFlag       string    `gorm:"column:cip_flag;type:varchar(20);not null" json:"cip_flag"` // CIP / NON_CIP
	Email         string    `gorm:"column:email;type:varchar(150)" json:"email"`
	BankKey       string    `gorm:"column:bank_key;type:varchar(100)" json:"bank_key"`
	BankAccount   string    `gorm:"column:bank_account;type:varchar(100)" json:"bank_account"`
	AccountHolder string    `gorm:"column:account_holder;type:varchar(150)" json:"account_holder"`
	Description   string    `gorm:"column:description;type:varchar(255)" json:"description"`
	IsActive      bool      `gorm:"column:is_active;default:true" json:"is_active"`
	CreatedAt     time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (VendorMaster) TableName() string {
	return "employee_vendor_mappings"
}
