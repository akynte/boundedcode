# MCP tool search

Model `qwen3.6-35b-a3b`, started 2026-10-29T09:12:46Z. 6 requests, each needing one of 60 tools of an MCP server.

| mcp_tool_search | Passed | Right tool | Model calls per request | Tool calls per request | Tool definitions (tokens) | Prompt tokens per request | Processed by the server per request | Median s |
|---|---|---|---|---|---|---|---|---|
| off | 12 / 12 | 12 / 12 | 2.0 | 1.0 | 10548 | 20768 | 8096 | 14.4 |
| on | 12 / 12 | 12 / 12 | 4.5 | 2.7 | 3748 | 21719 | 2564 | 13.1 |

| Case | Arm | Run | Passed | Tools | Answer |
|---|---|---|---|---|---|
| refund-status | off | 1 | true | mcp__ops__get_refund | Refund R7 has status **approved** with an amount of **12.50 EUR**. |
| refund-status | on | 1 | true | mcp_search, mcp_search, mcp__ops__search_refund, mcp__ops__list_refund, bash, bash, mcp__ops__get_refund | Refund **R7** has a status of **approved** with an amount of **12.50 EUR**. |
| shipment-location | off | 1 | true | mcp__ops__get_shipment | Shipment S-1009 is currently in **Rotterdam** with an ETA of **2026-11-02**. |
| shipment-location | on | 1 | true | mcp_search, ls, mcp__ops__get_shipment | Shipment **S-1009** is currently at **Rotterdam**, with an estimated arrival date of **2026-11-02**. |
| delete-alert | off | 1 | true | mcp__ops__delete_alert | Done — alert A-77 has been deleted. |
| delete-alert | on | 1 | true | mcp_search, mcp__ops__delete_alert | Alert A-77 has been deleted. Done. |
| metric-value | off | 1 | true | mcp__ops__get_metric | The current value of metric **M-cpu** is **73%** (unit: percent). |
| metric-value | on | 1 | true | mcp_search, mcp__ops__get_metric | The current value of metric **M-cpu** is **73%**. |
| user-email | off | 1 | true | mcp__ops__get_user | User U-5's email address is **dana@example.com**. |
| user-email | on | 1 | true | mcp_search, mcp__ops__get_user | User U-5's email address is **dana@example.com**. |
| invoice-total | off | 1 | true | mcp__ops__get_invoice | The total for invoice **INV-204** is **1,240.00 EUR** (status: paid). |
| invoice-total | on | 1 | true | mcp_search, mcp__ops__get_invoice | The total of invoice **INV-204** is **1,240.00 EUR** (status: paid). |
| refund-status | off | 2 | true | mcp__ops__get_refund | Refund **R7** has status **approved** with an amount of **12.50 EUR**. |
| refund-status | on | 2 | true | mcp_search, mcp__ops__get_refund | Refund **R7** has status **approved** with an amount of **12.50 EUR**. |
| shipment-location | off | 2 | true | mcp__ops__get_shipment | Shipment S-1009 is currently in **Rotterdam**, with an estimated arrival date of **2026-11-02**. |
| shipment-location | on | 2 | true | mcp_search, mcp__ops__get_shipment | According to the operations system, shipment **S-1009** is currently at **Rotterdam**, with an estimated arrival (ETA) o… |
| delete-alert | off | 2 | true | mcp__ops__delete_alert | Alert A-77 has been deleted. |
| delete-alert | on | 2 | true | mcp_search, mcp__ops__delete_alert | Alert A-77 has been deleted from the operations system. |
| metric-value | off | 2 | true | mcp__ops__get_metric | The current value of metric **M-cpu** is **73%** (unit: percent). |
| metric-value | on | 2 | true | mcp_search, mcp__ops__get_metric | The current value of metric **M-cpu** is **73%**. |
| user-email | off | 2 | true | mcp__ops__get_user | User U-5's email address is **dana@example.com**. |
| user-email | on | 2 | true | mcp_search, mcp__ops__get_user | User U-5's email address is **dana@example.com**. |
| invoice-total | off | 2 | true | mcp__ops__get_invoice | The total of invoice **INV-204** is **1,240.00 EUR** (status: paid). |
| invoice-total | on | 2 | true | mcp_search, mcp_search, mcp__ops__search_invoice, mcp__ops__get_invoice | The total of invoice **INV-204** is **1,240.00 EUR** (status: paid). |
