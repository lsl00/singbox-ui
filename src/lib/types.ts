export interface ApiConfig {
  url: string;
  secret: string;
}

export interface VersionInfo {
  version: string;
  api_version: number;
}

export interface ServiceStatusInfo {
  status: number;
  error_message: string;
}

export interface StatusInfo {
  memory: number;
  goroutines: number;
  connections_in: number;
  connections_out: number;
  traffic_available: boolean;
  uplink: number;
  downlink: number;
  uplink_total: number;
  downlink_total: number;
}

export interface GroupInfo {
  tag: string;
  type: string;
  selectable: boolean;
  selected: string;
  is_expand: boolean;
  items: GroupItemInfo[];
}

export interface GroupItemInfo {
  tag: string;
  type: string;
  url_test_time: number;
  url_test_delay: number;
}

export interface ConnectionInfo {
  id: string;
  inbound: string;
  inbound_type: string;
  ip_version: number;
  network: string;
  source: string;
  destination: string;
  domain: string;
  protocol: string;
  user: string;
  from_outbound: string;
  created_at: number;
  closed_at: number;
  uplink: number;
  downlink: number;
  uplink_total: number;
  downlink_total: number;
  rule: string;
  outbound: string;
  outbound_type: string;
  chain_list: string[];
}

export interface ConnectionEventInfo {
  type: number;
  id: string;
  connection: ConnectionInfo | null;
  uplink_delta: number;
  downlink_delta: number;
  closed_at: number;
}

export interface ConnectionEventsInfo {
  events: ConnectionEventInfo[];
  reset: boolean;
}

export interface LogEntryInfo {
  level: number;
  message: string;
}

export interface LogsInfo {
  messages: LogEntryInfo[];
  reset: boolean;
}

export interface ClashModeInfo {
  mode_list: string[];
  current_mode: string;
}

export const SERVICE_STATUS = [
  "Idle",
  "Starting",
  "Started",
  "Stopping",
  "Fatal",
] as const;

export const LOG_LEVELS = [
  "Panic",
  "Fatal",
  "Error",
  "Warning",
  "Info",
  "Debug",
  "Trace",
] as const;
