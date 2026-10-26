INSERT INTO sap_endpoints (service_code, service_name, scenario_id, odata_version, base_url, endpoint_path, username, password)
VALUES 
(
	'PO_CHANGE_HEADER',
	'Purchase Order Change Header (PATCH OData v4)',
	'SAP_COM_0053',
	'v4',
	'https://my445160-api.s4hana.cloud.sap',
	'/sap/opu/odata4/sap/api_purchaseorder_2/srvd_a2x/sap/purchaseorder/0001/PurchaseOrder',
	'CU_SAP_COM_0053',
	'PowerofOne@123456789'
),
(
	'PO_CHANGE_ITEM',
	'Purchase Order Change Item (PATCH OData v4)',
	'SAP_COM_0053',
	'v4',
	'https://my445160-api.s4hana.cloud.sap',
	'/sap/opu/odata4/sap/api_purchaseorder_2/srvd_a2x/sap/purchaseorder/0001/PurchaseOrderItem',
	'CU_SAP_COM_0053',
	'PowerofOne@123456789'
),
(
	'GR_WITH_PO',
	'Goods Receipt / Material Document with PO (OData v2)',
	'SAP_COM_0108',
	'v2',
	'https://my445160-api.s4hana.cloud.sap',
	'/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader',
	'Y_BPI_CU_SAP_COM_0108',
	'PowerofOne@123456789'
),
(
	'GR_NON_PO',
	'Goods Receipt Non PO (OData v2)',
	'SAP_COM_0108',
	'v2',
	'https://my445160-api.s4hana.cloud.sap',
	'/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader',
	'Y_BPI_CU_SAP_COM_0108',
	'PowerofOne@123456789'
),
(
	'MATERIAL_DOC_DETAIL',
	'Material Document Header & Item Query (OData v2)',
	'SAP_COM_0108',
	'v2',
	'https://my445160-api.s4hana.cloud.sap',
	'/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader',
	'Y_BPI_CU_SAP_COM_0108',
	'PowerofOne@123456789'
),
(
	'GL_ACCOUNT_LINE_ITEMS',
	'G/L Account Line Items Query (OData v2)',
	'SAP_COM_0303',
	'v2',
	'https://my445160-api.s4hana.cloud.sap',
	'/sap/opu/odata/sap/API_GLACCOUNTLINEITEM/GLAccountLineItem',
	'Y_BPI_CU_SAP_COM_0303',
	'PowerofOne@123456789'
)
ON CONFLICT (service_code) DO UPDATE 
SET username = EXCLUDED.username, 
    password = EXCLUDED.password, 
    base_url = EXCLUDED.base_url, 
    endpoint_path = EXCLUDED.endpoint_path, 
    odata_version = EXCLUDED.odata_version, 
    updated_at = NOW();
