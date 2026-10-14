import { createClient } from "@connectrpc/connect"
import { createConnectTransport } from "@connectrpc/connect-web"
import { DashboardService } from "@/gen/leaguelens/v1/dashboard_pb"

export const dashboard = createClient(DashboardService, createConnectTransport({ baseUrl: "/api" }))
