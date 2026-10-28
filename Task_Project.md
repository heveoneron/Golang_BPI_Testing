📋 List Task : Golang_BPI_Testing

[V] Penambahan List API baru untuk mapping antara maximo ke golang dan ke SAP Grow, di middleware dan middleware-panel
#####========================Change PO Header=========================================#####

Method PATCH
https://my445160-api.s4hana.cloud.sap/sap/opu/odata4/sap/api_purchaseorder_2/srvd_a2x/sap/purchaseorder/0001/PurchaseOrder('4100000032')

contoh Payload data yang mau di change :
{
    "PaymentTerms": "0001"
}

#####========================Change PO Item=========================================#####

Method PATCH
https://my445160-api.s4hana.cloud.sap/sap/opu/odata4/sap/api_purchaseorder_2/srvd_a2x/sap/purchaseorder/0001/PurchaseOrderItem(PurchaseOrder='4100000032',PurchaseOrderItem='1')

payload :
{
    "PurchaseOrderItemText": "Testing API chenge PO Item Text"
}

#####========================Material Document/GR with PO and non PO=========================================#####
mas hafiz, ini contoh OData V2 API Material Document/GR with PO and non PO di tenant starter :
Basic Auth :
Username : Y_BPI_CU_SAP_COM_0108
Password : PowerofOne@123456789

GET Material Document
https://my445160-api.s4hana.cloud.sap/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader

GET Material Document Detail
https://my445160-api.s4hana.cloud.sap/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader(MaterialDocumentYear='2025',MaterialDocument='5000000000')/to_MaterialDocumentItem

POST GR with PO
https://my445160-api.s4hana.cloud.sap/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader

contoh payload GR with PO :
{
    "DocumentDate": "/Date(1792108800000)/",
    "PostingDate": "/Date(1792108800000)/",
    "GoodsMovementCode": "01",
    "MaterialDocumentHeaderText": "Testing GR with PO",
    "to_MaterialDocumentItem": {
        "results": [
            {
                "GoodsMovementType": "101",
                "GoodsMovementRefDocType": "B",
                "PurchaseOrder": "4500000010",
                "PurchaseOrderItem": "10",
                "Material": "PK0514",
                "Plant": "5110",
                "EntryUnit": "PC",
                "QuantityInEntryUnit": "1"
            }
        ]
    }
}

POST GR non PO
https://my445160-api.s4hana.cloud.sap/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader

contoh payload GR non PO :
{
    "DocumentDate": "/Date(1792108800000)/",
    "PostingDate": "/Date(1792108800000)/",
    "GoodsMovementCode": "05",
    "MaterialDocumentHeaderText": "Testing GR Non-PO Mvt 501",
    "to_MaterialDocumentItem": {
        "results": [
            {
                "GoodsMovementType": "501",
                "Material": "PK0514",
                "Plant": "5110",
                "StorageLocation": "511C",
                "EntryUnit": "PC",
                "QuantityInEntryUnit": "10"
            }
        ]
    }
}




#######================G/L Account Line Items================#######
mas hafiz, ini contoh OData V2 API G/L Account Line Items di tenant starter :
Basic Auth :
Username : Y_BPI_CU_SAP_COM_0303
Password : PowerofOne@123456789

GET G/L Account Line Items
https://my445160-api.s4hana.cloud.sap/sap/opu/odata/sap/API_GLACCOUNTLINEITEM/GLAccountLineItem

GET G/L Account Line Items Detail
https://my445160-api.s4hana.cloud.sap/sap/opu/oda  ta/sap/API_GLACCOUNTLINEITEM/GLAccountLineItem('.1~0E.2~5110.3~2026.4~PA00000010.5~000001.6~0E')



[V] Pembuatan API Middleware dengan method Post, yang baru untuk mengirim data Exchage Rate yang akan di hit oleh maximo, buatkan juga table di database PostgreSQL dan di log juga Menu baru untuk upload data Exchage Rate di web middleware-panel.
{
  "event": "kurs.updated",
  "sent_at": "2026-10-22T13:54:00+07:00",
  "source": "kemenkeu-kurs-pajak",
  "period": {
    "kmk_number": "CONTOH-KMK",
    "valid_from": "2026-10-22",
    "valid_to": "2026-10-28",
    "document_url": "https://contoh-url-dokumen"
  },
  "rates": [
    {
      "currency_code": "USD",
      "currency_name": "Dolar Amerika Serikat",
      "rate": 16500,
      "unit": 1,
      "rate_per_unit": "16500.000000",
      "change": 25
    },
    {
      "currency_code": "JPY",
      "currency_name": "Yen Jepang",
      "rate": 11000,
      "unit": 100,
      "rate_per_unit": "110.000000",
      "change": null
    }
  ]
}

[V] pembuatan master data untuk mapping UOM antara UOM yang di kirim kan Maximo ke UOM yang ada di Sistem SAP, buatkan table di PostgreSQL dan juga menunya di web middleware-panel.
Contoh Data UOM :
Maximo : PC -> SAP : PCE
Maximo : EA -> SAP : PCE
Maximo : MT -> SAP : MAT
Maximo : KG -> SAP : KGM

[V] penambahan api Get untuk cek detail Material Document/GR dari Sistem SAP Grow ke middleware-panel.
Method GET
https://my445160-api.s4hana.cloud.sap/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader('202500000001')

contoh response data material document : 
{
    "d": {
        "MaterialDocument": "202500000001",
        "MaterialDocumentYear": "2025",
        "to_MaterialDocumentItem": {
            "results": [
                {
                    "MaterialDocument": "202500000001",
                    "MaterialDocumentItem": "00001",
                    "GoodsMovementType": "101",
                    "GoodsMovementCode": "01",
                    "MaterialDocumentHeaderText": "Testing SAP API Get GR with PO",
                    "MtrlDocItemCategory": "",
                    "MaterialDocumentYearRefDocType": "",
                    "PurchaseOrder": "4500000010",
                    "PurchaseOrderItem": "10",
                    "Material": "PK0514",
                    "Plant": "5110",
                    "StorageLocation": "",
                    "EntryUnit": "PC",
                    "QuantityInEntryUnit": "1"
                }
            ]
        }
    }
}

[V] Pembuatan konfigurasi Docker & Docker Compose (`docker.yml` & `docker-compose.yml`) siap pakai di server:
- Multi-stage Dockerfile untuk middleware engine (`bpi-middleware`, port 8085)
- Multi-stage Dockerfile untuk web panel UI (`bpi-middleware-panel`, port 8091)
- Database container PostgreSQL 16 (`bpi-postgres`, port 5432) dengan inisialisasi schema DDL otomatis
- Vendored offline Go build (`-mod=vendor`) agar build instan tanpa kendala jaringan proxy/timeout
- Konfigurasi terpusat di `docker.yml` tanpa perlu konfigurasi manual per folder.