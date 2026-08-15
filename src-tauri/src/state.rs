use serde::{Deserialize, Serialize};
use std::sync::atomic::AtomicBool;
use std::sync::Arc;
use tokio::sync::RwLock;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ApiConfig {
    pub url: String,
    pub secret: String,
}

impl Default for ApiConfig {
    fn default() -> Self {
        Self {
            url: "http://localhost:9000".to_string(),
            secret: String::new(),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct VersionInfo {
    pub version: String,
    pub api_version: i32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ServiceStatusInfo {
    pub status: i32,
    pub error_message: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StatusInfo {
    pub memory: u64,
    pub goroutines: i32,
    pub connections_in: i32,
    pub connections_out: i32,
    pub traffic_available: bool,
    pub uplink: i64,
    pub downlink: i64,
    pub uplink_total: i64,
    pub downlink_total: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GroupInfo {
    pub tag: String,
    #[serde(rename = "type")]
    pub group_type: String,
    pub selectable: bool,
    pub selected: String,
    pub is_expand: bool,
    pub items: Vec<GroupItemInfo>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GroupItemInfo {
    pub tag: String,
    #[serde(rename = "type")]
    pub item_type: String,
    pub url_test_time: i64,
    pub url_test_delay: i32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ConnectionInfo {
    pub id: String,
    pub inbound: String,
    pub inbound_type: String,
    pub ip_version: i32,
    pub network: String,
    pub source: String,
    pub destination: String,
    pub domain: String,
    pub protocol: String,
    pub user: String,
    pub from_outbound: String,
    pub created_at: i64,
    pub closed_at: i64,
    pub uplink: i64,
    pub downlink: i64,
    pub uplink_total: i64,
    pub downlink_total: i64,
    pub rule: String,
    pub outbound: String,
    pub outbound_type: String,
    pub chain_list: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ConnectionEventInfo {
    #[serde(rename = "type")]
    pub event_type: i32,
    pub id: String,
    pub connection: Option<ConnectionInfo>,
    pub uplink_delta: i64,
    pub downlink_delta: i64,
    pub closed_at: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ConnectionEventsInfo {
    pub events: Vec<ConnectionEventInfo>,
    pub reset: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LogEntryInfo {
    pub level: i32,
    pub message: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LogsInfo {
    pub messages: Vec<LogEntryInfo>,
    pub reset: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ClashModeInfo {
    pub mode_list: Vec<String>,
    pub current_mode: String,
}

impl From<crate::grpc::client::StatusData> for StatusInfo {
    fn from(s: crate::grpc::client::StatusData) -> Self {
        Self {
            memory: s.memory,
            goroutines: s.goroutines,
            connections_in: s.connections_in,
            connections_out: s.connections_out,
            traffic_available: s.traffic_available,
            uplink: s.uplink,
            downlink: s.downlink,
            uplink_total: s.uplink_total,
            downlink_total: s.downlink_total,
        }
    }
}

pub struct AppState {
    pub config: RwLock<ApiConfig>,
    pub client: RwLock<Option<Arc<crate::grpc::client::GrpcClient>>>,
    pub quitting: AtomicBool,
}

impl AppState {
    pub fn new() -> Self {
        Self {
            config: RwLock::new(ApiConfig::default()),
            client: RwLock::new(None),
            quitting: AtomicBool::new(false),
        }
    }
}
