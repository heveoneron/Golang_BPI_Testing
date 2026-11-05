Purchase Orders (PO) - Maximo to SAP																
																
MAXIMO									SAP							
No	Column Name	Source System Field Name	Data Type	Max Data Length	Constant Value	Description	Mandatory / Optional		No	Column Name	Source System Field Name	Data Type	Max Data Length	Constant Value	Description	Mandatory / Optional
1	PONUM	PO.PONUM	UPPER	20		PO NUMBER	M		1	/PurchaseOrder	PurchaseOrder				"Need conversion if PO number kontrak
cth: (4700000000-10)"	M
										/PurchaseOrder	SupplierRespSalesPersonName				If PO kontrak (4100000567-25) nomo PO tersebut di taruh di fields ini	
2									2	/PurchaseOrder	PurchaseOrderType				"If Prefix PO 45 = ZPRC
Else If Prefix PO 47 = ZCTR"	M
4	CURRENCYCODE	PO.CURRENCYCODE	UPPER	8					4	/PurchaseOrder	DocumentCurrency					M
										/PurchaseOrder/{PurchaseOrder}/_PurchaseOrderItem	DocumentCurrency					
5	VENDOR	PO.VENDOR	UPPER	12			M		5	/PurchaseOrder	Supplier					M
7	ORDERDATE	PO.ORDERDATE	DATE TIME	10		ORDER DATE	M		7	/PurchaseOrder	PurchaseOrderDate					M
										/PurchaseOrder	CompanyCode			1000	Hardcode	M
										/PurchaseOrder	PurchasingOrganization			BPI		M
										/PurchaseOrder	PurchasingGroup			001		M
	Info DP									/PurchaseOrder	DownPaymentPercentageOfTotAmt					
										/PurchaseOrder	DownPaymentType			V	Hardcode	
										/PurchaseOrder	DownPaymentDueDate					
8	STATUS	PO.STATUS	UPPER	12					8	/PurchaseOrder/{PurchaseOrder}/_PurchaseOrderItem	IsCompletelyDelivered				"If Status = Closed / cancelled
--> Value = True"	
9	STATUSDATE	PO.STATUSDATE	DATE TIME	10					9	StatusDate	PurchTable.BPI_MXStatusDate					
									10							
11	POLINENUM	POLINE.POLINENUM	INTEGER	12		LINE NUMBER	M		11	/PurchaseOrder/{PurchaseOrder}/_PurchaseOrderItem	PurchaseOrderItem					
										/PurchaseOrder/{PurchaseOrder}/_PurchaseOrderItem	AccountAssignmentCategory			K	Hardcode = K	
12	ITEMNUM	POLINE.ITEMNUM	UPPER	30		ITEM CODE	M		12	/PurchaseOrder/{PurchaseOrder}/_PurchaseOrderItem	Material					M
	pol_desc_ld									/PurchaseOrder/{PurchaseOrder}/_PurchaseOrderItem	PurchaseOrderItemText				"Taruh di kedua field tsb
Di orderitemtext (maks 40 character, perlu dipotong middleware)
Taruh full desc nya di plain long text"	
										"_PurchaseOrderItemNote"	PlainLongText 					
13	ORDERUNIT	POLINE.ORDERUNIT	UPPER	16		ORDER UNIT			13	/PurchaseOrder/{PurchaseOrder}/_PurchaseOrderItem	PurchaseOrderQuantityUnit				Diperlukan conversion UoM di Middleware	
14	QTY	POLINE.ORDERQTY	DECIMAL	15		QTY ORDER FOR EACH ITEM	M		14	/PurchaseOrder/{PurchaseOrder}/_PurchaseOrderItem	OrderQuantity					M
15	SITEID								15	/PurchaseOrder/{PurchaseOrder}/_PurchaseOrderItem	Plant					
15	UNITCOST	POLINE.UNITCOST	DECIMAL 	10		UNIT COST FOR EACH ITEM	M		15	/PurchaseOrder/{PurchaseOrder}/_PurchaseOrderItem	NetPriceAmount					M
19	BPIBUDGETCODE	POLINE.GL	ALN	25		Debit /Credit Account Segment Budget Code			19	/PurchaseOrderItem/{PurchaseOrder}/{PurchaseOrderItem}/_PurOrdAccountAssignment	CostCenter					
										/PurchaseOrder/{PurchaseOrder}/_PurchaseOrderItem	InvoiceIsGoodsReceiptBased			TRUE	Hardcode = True	
										/PurchaseOrder/{PurchaseOrder}/_PurchaseOrderItem	TaxCode			V0	Hardcode = V0	
25	BPISTATUSIFACE	BPISTATUSIFACE	SMALLINT	10	0	Status for the data that already sent to Axapta			25	Tidak ketarik golang						
26	TAXCODE1	POLINE.TAXCODE1	UPPER	8					26	/PurchaseOrderItem/{PurchaseOrder}/{PurchaseOrderItem}/_PurOrdPricingElement	ConditionRateAmount				"Butuh Conversion
PPN11 = 11"	
											ConditionType			DRG1	akan hardcode DRG1	
											ConditionRateRatioUnit			%	Hardcode = %	
27	REVISIONNUM	PO.REVISIONNUM	INTEGER	12					27	For Golang purposes (info apakah itu create atau update)						


gw mau kasi config akhir buat api PO bro

ini kondisi kalo po nya mau di edit 
Saat PO mau di change, perlu validasi material document nya	cek material document, search by PO yang mau di change
	if no matdoc found = PO bisa di change
	If matdoc found = jumlahkan seluruh qty matdoc 101 | 102 | 122
	"kemudian jumlahkan
101 - 102 - 122 = Hasil"
	If Hasil > 0 = PO do not change
	if hasil = 0 = PO monggo di change

terus ini body json nya buat kirim ke SAP nya


yang pertama body untuk json create Normal PO
dengan kondisi TaxCode1 kosong
Post Payload
{
   "PurchaseOrder": "4700012063",
   "PurchaseOrderType": "ZBPI", //hardcode
   "PurchaseOrderDate": "2026-01-01",
"   ""CompanyCode"": ""5110"",
   ""SupplierRespSalesPersonName"": ""4700000012-63"""
   "PurchasingOrganization": "5110",
   "PurchasingGroup": "001",
   "Supplier": "VND-001",
   "DocumentCurrency": "IDR",
   "_PurchaseOrderItem": [
      {
         "PurchaseOrderItem": "1",
         "Material": "SP006",
         "Plant": "5110",
         "AccountAssignmentCategory": "K", //hardcode
         "DocumentCurrency": "IDR",
         "PurchaseOrderItemText":"Looooooooooooooong Descriptiooooooooooo",
         "PurchaseOrderQuantityUnit": "EA",
         "OrderPriceUnit": "EA",
         "NetPriceAmount": 100000,

         "OrderQuantity": 10,
         "TaxCode": "V0", //hardcode

         "InvoiceIsGoodsReceiptBased": true, //hardcode
         "_PurOrdAccountAssignment":[{
        "PurchaseOrder": "4700012063",
        "PurchaseOrderItem": "1",
        "AccountAssignmentNumber": "1",
        "CostCenter": "51101101"
    }],
        "_PurchaseOrderItemNote":[{
        "PurchaseOrder": "4700012063",
        "PurchaseOrderItem": "1",
        "TextObjectType": "F01", //hardcode
        "Language": "EN", //hardcode
        "PlainLongText": "Looooooooooooooong Descriptiooooooooooooooooon"    
    }]    
      }
   ]
}

terus
yang kedua body untuk json create PO with Tax
TaxCode1 terisi maka ConditionType dan ConditionRatioUnit terisi hardcode dan ConditionRateAmount itu convert TaxCode1 dari maximo jika dikirim PPN11 maka jadi 11 yang di isi di ConditionRateAmount begitu pula jika PPN10 maka di isi 10.
Post Payload
{
    "PurchaseOrder": "4100001371",
    "PurchaseOrderType": "NB",
    "CompanyCode": "5110",
"    ""PurchasingOrganization"": ""5110"",
    ""SupplierRespSalesPersonName"": ""4700000012-63"""
    "PurchasingGroup": "001",
    "Supplier": "51300082",
    "DocumentCurrency": "IDR",
    "_PurchaseOrderItem": [
        {
            "PurchaseOrderItem": "1",
            "DocumentCurrency": "IDR",
            "Material": "SP006",
            "PurchaseOrderItemText": "gta 6 november 19",
            "AccountAssignmentCategory": "K",
            "Plant": "5110",
            "NetPriceAmount": 1235678,
            "OrderQuantity": 20.0,
            "NetPriceQuantity": 1,
            "PurchaseOrderQuantityUnit": "PC",
            "OrderPriceUnit": "PC",
            "InvoiceIsGoodsReceiptBased": true,
            "_PurOrdAccountAssignment":[
    {
        "PurchaseOrder": "4100001371",
        "PurchaseOrderItem": "1",
        "AccountAssignmentNumber": "10",
        "CostCenter": "51101101"
    }
   ],
            "_PurchaseOrderItemNote":[
    {
        "PurchaseOrder": "4100001371",
        "PurchaseOrderItem": "1",
        "TextObjectType": "F01",
        "Language": "EN",
        "PlainLongText": "gta 6 november 19"    
    }
        ],
            "_PurOrdPricingElement": [
                {
                    "PurchaseOrderItem": "1",
                    "ConditionType": "DRG1",
                    "ConditionRateAmount": 11,
                    "ConditionRateRatioUnit": "%"
                },
                {
                    "PurchaseOrderItem": "1",
                    "ConditionType": "PMP0",
                    "ConditionRateAmount":1235678
                }
            ]
        }
    ]
}

terus
yang ketiga body untuk json create PO with DP
jika info DP terisi maka DownPaymentPercentageOfTotAmt, DownPaymentType, DownPaymentDueDate terisi, saat ini DP belum ada field nya dari maximo, sementara lu buat aja dulu biar nanti gampang di edit nya. dan nanti field yang di kasih maximo itu rate dp persennya dan tanggal duedate nya saja.

Post Payload
{
   "PurchaseOrder": "4100000272",
   "PurchaseOrderType": "NB",
   "PurchaseOrderDate": "2026-01-01",
"   ""CompanyCode"": ""5110"",
   ""SupplierRespSalesPersonName"": ""4700000012-63"""
   "PurchasingOrganization": "5110",
   "PurchasingGroup": "001",
   "Supplier": "VND-001",
   "DocumentCurrency": "USD",
   "DownPaymentType": "V",
    "DownPaymentPercentageOfTotAmt": 20.00,
    "DownPaymentDueDate": "2026-11-30",
   "_PurchaseOrderItem": [
      {
         "PurchaseOrderItem": "1",
         "Material": "SP006",
         "Plant": "5110",
         "AccountAssignmentCategory": "K",
         "PurchaseOrderItemText":"Barang Deskripsi",
         "PurchaseOrderQuantityUnit": "EA",
         "OrderPriceUnit": "EA",
         "NetPriceAmount": 12346,
         "DocumentCurrency": "USD",
         "OrderQuantity": 10,
         "TaxCode": "V0",
         "InvoiceIsGoodsReceiptBased": true,
         "_PurOrdAccountAssignment":[
    {
        "PurchaseOrder": "4100000272",
        "PurchaseOrderItem": "1",
        "AccountAssignmentNumber": "1",
        "CostCenter": "51101101"
    }
   ],
        "_PurchaseOrderItemNote":[{
        "PurchaseOrder": "4100000272",
        "PurchaseOrderItem": "1",
        "TextObjectType": "F01",
        "Language": "EN",
        "PlainLongText": "Barang Deskripsi"    
    }] 
      }
]
}


terus
yang ke empat itu gabungan antara PO with Tax dan DP
yang ke empat itu gabungan antara PO with Tax dan DP
untuk body json nya belum ada, lu bantu buatin yak buat kirim nya nanti gampang,

Post Payload (PO with Tax & DP)
{
    "PurchaseOrder": "4100001372",
    "PurchaseOrderType": "NB",
    "CompanyCode": "5110",
    "PurchasingOrganization": "5110",
    "SupplierRespSalesPersonName": "4700000012-63",
    "PurchasingGroup": "001",
    "Supplier": "51300082",
    "DocumentCurrency": "IDR",
    "PurchaseOrderDate": "2026-01-01",
    "DownPaymentType": "V",
    "DownPaymentPercentageOfTotAmt": 20.00,
    "DownPaymentDueDate": "2026-11-30",
    "_PurchaseOrderItem": [
        {
            "PurchaseOrderItem": "1",
            "DocumentCurrency": "IDR",
            "Material": "SP006",
            "PurchaseOrderItemText": "Barang Deskripsi",
            "AccountAssignmentCategory": "K",
            "Plant": "5110",
            "NetPriceAmount": 1235678,
            "OrderQuantity": 20.0,
            "NetPriceQuantity": 1,
            "PurchaseOrderQuantityUnit": "PC",
            "OrderPriceUnit": "PC",
            "InvoiceIsGoodsReceiptBased": true,
            "_PurOrdAccountAssignment": [
                {
                    "PurchaseOrder": "4100001372",
                    "PurchaseOrderItem": "1",
                    "AccountAssignmentNumber": "1",
                    "CostCenter": "51101101"
                }
            ],
            "_PurchaseOrderItemNote": [
                {
                    "PurchaseOrder": "4100001372",
                    "PurchaseOrderItem": "1",
                    "TextObjectType": "F01",
                    "Language": "EN",
                    "PlainLongText": "Barang Deskripsi Lengkap Maximo"
                }
            ],
            "_PurOrdPricingElement": [
                {
                    "PurchaseOrderItem": "1",
                    "ConditionType": "DRG1",
                    "ConditionRateAmount": 11,
                    "ConditionRateRatioUnit": "%"
                },
                {
                    "PurchaseOrderItem": "1",
                    "ConditionType": "PMP0",
                    "ConditionRateAmount": 1235678
                }
            ]
        }
    ]
}

url nya tetep sama pake url api ini :
ini contoh OData V4 API PO di tenant starter :
Basic Auth :
Username : CU_SAP_COM_0053
Password : PowerofOne@123456789

GET PO HEADER
https://my445160-api.s4hana.cloud.sap/sap/opu/odata4/sap/api_purchaseorder_2/srvd_a2x/sap/purchaseorder/0001/PurchaseOrder

GET PO ITEM
https://my445160-api.s4hana.cloud.sap/sap/opu/odata4/sap/api_purchaseorder_2/srvd_a2x/sap/purchaseorder/0001/PurchaseOrderItem

POST PO
https://my445160-api.s4hana.cloud.sap/sap/opu/odata4/sap/api_purchaseorder_2/srvd_a2x/sap/purchaseorder/0001/PurchaseOrder

Note: - jika ingin melakukan POST maka di perlukan token x-csrf-token
- yang dimana di dapatkan dari method GET, dan di berikan di HEADER dengan key x-csrf-token dan Value nya fetch, lalu setelah di kirim maka akan muncul di resposne HEADER di bagian x-csrf-token dengan value nya
- lalu saat ingin melakukan POST maka di HEADER perlu di masukan value nya hasil dari GET nya. 

kalo uda done lu kasi centang ini disini yak[x]