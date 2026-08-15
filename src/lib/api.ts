import { invoke } from "@tauri-apps/api/core";
import type {
  ApiConfig,
  ClashModeInfo,
  ConnectionEventsInfo,
  GroupInfo,
  LogsInfo,
  ServiceStatusInfo,
  StatusInfo,
  VersionInfo,
} from "./types";

export function connect(config: ApiConfig): Promise<VersionInfo> {
  return invoke("connect", { config });
}

export function disconnect(): Promise<void> {
  return invoke("disconnect");
}

export function getStatus(): Promise<StatusInfo> {
  return invoke("get_status");
}

export function getStartedAt(): Promise<number> {
  return invoke("get_started_at");
}

export function getServiceStatus(): Promise<ServiceStatusInfo> {
  return invoke("get_service_status");
}

export function getClashModeStatus(): Promise<ClashModeInfo> {
  return invoke("get_clash_mode_status");
}

export function setClashMode(mode: string): Promise<void> {
  return invoke("set_clash_mode", { mode });
}

export function getGroups(): Promise<GroupInfo[]> {
  return invoke("get_groups");
}

export function selectOutbound(
  groupTag: string,
  outboundTag: string,
): Promise<void> {
  return invoke("select_outbound", {
    groupTag,
    outboundTag,
  });
}

export function urlTest(outboundTag: string): Promise<void> {
  return invoke("url_test", { outboundTag });
}

export function setGroupExpand(
  groupTag: string,
  isExpand: boolean,
): Promise<void> {
  return invoke("set_group_expand", { groupTag, isExpand });
}

export function getConnections(): Promise<ConnectionEventsInfo[]> {
  return invoke("get_connections");
}

export function closeConnection(id: string): Promise<void> {
  return invoke("close_connection", { id });
}

export function closeAllConnections(): Promise<void> {
  return invoke("close_all_connections");
}

export function getLogs(): Promise<LogsInfo[]> {
  return invoke("get_logs");
}

export function clearLogs(): Promise<void> {
  return invoke("clear_logs");
}
