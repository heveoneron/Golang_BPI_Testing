package repository

import (
	"middleware-panel/models"
	"strings"

	"gorm.io/gorm"
)

type TableAPIMappingRepository interface {
	GetAll() ([]models.TableAPIMapping, error)
	GetByID(id uint) (*models.TableAPIMapping, error)
	Save(m *models.TableAPIMapping) error
	ToggleActive(id uint) error
	Delete(id uint) error
	SeedDefaults() error
}

type tableAPIMappingRepo struct {
	db *gorm.DB
}

func NewTableAPIMappingRepository(db *gorm.DB) TableAPIMappingRepository {
	return &tableAPIMappingRepo{db: db}
}

func (r *tableAPIMappingRepo) GetAll() ([]models.TableAPIMapping, error) {
	var list []models.TableAPIMapping
	err := r.db.Order("id asc").Find(&list).Error
	return list, err
}

func (r *tableAPIMappingRepo) GetByID(id uint) (*models.TableAPIMapping, error) {
	var item models.TableAPIMapping
	err := r.db.First(&item, id).Error
	return &item, err
}

func (r *tableAPIMappingRepo) Save(m *models.TableAPIMapping) error {
	m.SourceTable = strings.ToUpper(strings.TrimSpace(m.SourceTable))
	m.TargetServiceCode = strings.ToUpper(strings.TrimSpace(m.TargetServiceCode))

	if m.ID > 0 {
		return r.db.Save(m).Error
	}

	var existing models.TableAPIMapping
	err := r.db.Where("UPPER(source_table) = UPPER(?)", m.SourceTable).First(&existing).Error
	if err == nil {
		m.ID = existing.ID
		return r.db.Model(&existing).Updates(m).Error
	}

	return r.db.Create(m).Error
}

func (r *tableAPIMappingRepo) ToggleActive(id uint) error {
	var item models.TableAPIMapping
	if err := r.db.First(&item, id).Error; err != nil {
		return err
	}
	return r.db.Model(&item).Update("is_active", !item.IsActive).Error
}

func (r *tableAPIMappingRepo) Delete(id uint) error {
	return r.db.Delete(&models.TableAPIMapping{}, id).Error
}

func (r *tableAPIMappingRepo) SeedDefaults() error {
	var count int64
	r.db.Model(&models.TableAPIMapping{}).Count(&count)
	if count > 0 {
		return nil
	}

	defaults := []models.TableAPIMapping{
		{
			SourceTable:       "ITEM",
			SourceDesc:        "Master Item, Material & Spare Parts",
			TargetServiceCode: "PRODUCT",
			Direction:         "OUTBOUND",
			SyncMode:          "BATCH_SCHEDULE",
			IsActive:          true,
			Notes:             "Sinkronisasi master data item/material dari Maximo ke SAP Grow (API_PRODUCT_SRV)",
		},
		{
			SourceTable:       "PO",
			SourceDesc:        "Purchase Order Header & Lines (mxout_inter_trans)",
			TargetServiceCode: "PO",
			Direction:         "OUTBOUND",
			SyncMode:          "BATCH_SCHEDULE",
			IsActive:          true,
			Notes:             "Dokumen pemesanan barang dari Maximo ke SAP Grow S/4HANA Cloud (PurchaseOrder v4)",
		},
		{
			SourceTable:       "COMPANIES",
			SourceDesc:        "Vendor, Supplier & Business Partner",
			TargetServiceCode: "BP",
			Direction:         "OUTBOUND",
			SyncMode:          "REALTIME",
			IsActive:          true,
			Notes:             "Sinkronisasi mitra bisnis & supplier dari Maximo ke SAP Business Partner (API_BUSINESS_PARTNER v2)",
		},
		{
			SourceTable:       "MATRECTRANS",
			SourceDesc:        "Goods Receipt (GRN) & Material Movements",
			TargetServiceCode: "PO",
			Direction:         "OUTBOUND",
			SyncMode:          "MANUAL",
			IsActive:          true,
			Notes:             "Penerimaan barang fisik dari gudang Maximo terhadap PO SAP Grow",
		},
		{
			SourceTable:       "PR",
			SourceDesc:        "Purchase Requisition (Permintaan Pembelian)",
			TargetServiceCode: "PO",
			Direction:         "OUTBOUND",
			SyncMode:          "MANUAL",
			IsActive:          false,
			Notes:             "Pengajuan PR sebelum dikonversi menjadi PO di SAP",
		},
		{
			SourceTable:       "AXUOM",
			SourceDesc:        "Unit of Measure (UOM) Standar",
			TargetServiceCode: "PRODUCT",
			Direction:         "OUTBOUND",
			SyncMode:          "MANUAL",
			IsActive:          true,
			Notes:             "Katalog satuan ukuran (UOM) Maximo disinkronkan ke Product Master UOM SAP",
		},
	}

	for _, d := range defaults {
		_ = r.db.Create(&d).Error
	}
	return nil
}
