package models

import "time"

// VendorBankAccount merepresentasikan detail rekening bank vendor (11 field dari OData SAP to_BusinessPartnerBank)
type VendorBankAccount struct {
	ID                       int64     `gorm:"primaryKey;column:id;autoIncrement" json:"id"`
	VendorCode               string    `gorm:"column:vendor_code;type:varchar(50);not null;index" json:"vendor_code"`
	BankIdentification       string    `gorm:"column:bank_identification;type:varchar(20);not null" json:"bank_identification"`        // 4. <d:BankIdentification>
	BankCountryKey           string    `gorm:"column:bank_country_key;type:varchar(10)" json:"bank_country_key"`                      // 5. <d:BankCountryKey>
	BankNumber               string    `gorm:"column:bank_number;type:varchar(50)" json:"bank_number"`                                // 1. d:BankNumber (Bank Key)
	BankAccount              string    `gorm:"column:bank_account;type:varchar(100);not null" json:"bank_account"`                    // 2. d:BankAccount (Bank Account)
	SWIFTCode                string    `gorm:"column:swift_code;type:varchar(50)" json:"swift_code"`                                  // 6. <d:SWIFTCode>
	BankName                 string    `gorm:"column:bank_name;type:varchar(150)" json:"bank_name"`                                  // 7. <d:BankName>
	BankControlKey           string    `gorm:"column:bank_control_key;type:varchar(20)" json:"bank_control_key"`                      // 8. <d:BankControlKey>
	IBAN                     string    `gorm:"column:iban;type:varchar(50)" json:"iban"`                                              // 9. <d:IBAN>
	BankAccountReferenceText string    `gorm:"column:bank_account_reference_text;type:varchar(255)" json:"bank_account_reference_text"` // 10. <d:BankAccountReferenceText>
	BankAccountHolderName    string    `gorm:"column:bank_account_holder_name;type:varchar(150)" json:"bank_account_holder_name"`    // 3. d:BankAccountHolderName
	BankAccountName          string    `gorm:"column:bank_account_name;type:varchar(150)" json:"bank_account_name"`                  // 11. <d:BankAccountName>
	IsDefault                bool      `gorm:"column:is_default;default:false" json:"is_default"`
	CreatedAt                time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt                time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (VendorBankAccount) TableName() string {
	return "vendor_bank_accounts"
}

// VendorMaster merepresentasikan Master Data Vendor Maximo & SAP S/4HANA
// sesuai spesifikasi Outbound Master Data Vendor (CIP/Non-CIP, Email, dan Multiple Rekening Bank)
type VendorMaster struct {
	VendorCode    string              `gorm:"primaryKey;column:vendor_code;type:varchar(50)" json:"vendor_code"`
	CIPFlag       string              `gorm:"column:cip_flag;type:varchar(20);not null" json:"cip_flag"` // CIP / NON_CIP
	Email         string              `gorm:"column:email;type:varchar(150)" json:"email"`
	BankKey       string              `gorm:"column:bank_key;type:varchar(100)" json:"bank_key"`             // default/primary bank key
	BankAccount   string              `gorm:"column:bank_account;type:varchar(100)" json:"bank_account"`     // default/primary bank account
	AccountHolder string              `gorm:"column:account_holder;type:varchar(150)" json:"account_holder"` // default/primary account holder
	Description   string              `gorm:"column:description;type:varchar(255)" json:"description"`
	IsActive      bool                `gorm:"column:is_active;default:true" json:"is_active"`
	CreatedAt     time.Time           `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time           `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	BankAccounts  []VendorBankAccount `gorm:"foreignKey:VendorCode;references:VendorCode" json:"bank_accounts"`
}

func (VendorMaster) TableName() string {
	return "employee_vendor_mappings"
}
