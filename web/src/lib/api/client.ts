import { createClient } from "@connectrpc/connect"
import { createConnectTransport } from "@connectrpc/connect-web"
import { ConnectionService } from "@/gen/leaguelens/v1/connections_pb"
import { DashboardService } from "@/gen/leaguelens/v1/dashboard_pb"
import { SyncService } from "@/gen/leaguelens/v1/sync_pb"

const transport = createConnectTransport({ baseUrl: "/api" })

export const dashboard = createClient(DashboardService, transport)

export const sync = createClient(SyncService, transport)

export const connections = createClient(ConnectionService, transport)
