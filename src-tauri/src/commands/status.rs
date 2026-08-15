use std::sync::Arc;
use tauri::State;

use crate::grpc::client::GrpcClient;
use crate::state::{ApiConfig, AppState, ClashModeInfo, ServiceStatusInfo, StatusInfo, VersionInfo};

#[tauri::command]
pub async fn connect(config: ApiConfig, state: State<'_, AppState>) -> Result<VersionInfo, String> {
    let client = GrpcClient::new(&config)?;
    let version = client.get_version().await?;
    log::info!("connected to sing-box {} (api v{})", version.version, version.api_version);

    let mut config_lock = state.config.write().await;
    *config_lock = config;

    let mut client_lock = state.client.write().await;
    *client_lock = Some(Arc::new(client));

    Ok(version)
}

#[tauri::command]
pub async fn disconnect(state: State<'_, AppState>) -> Result<(), String> {
    let mut client_lock = state.client.write().await;
    *client_lock = None;
    Ok(())
}

#[tauri::command]
pub async fn get_status(state: State<'_, AppState>) -> Result<StatusInfo, String> {
    let client_lock = state.client.read().await;
    let client = client_lock
        .as_ref()
        .ok_or("Not connected to sing-box API")?;

    let statuses = client.subscribe_status().await?;
    let latest = statuses
        .into_iter()
        .last()
        .ok_or("No status data received")?;
    Ok(StatusInfo::from(latest))
}

#[tauri::command]
pub async fn get_version(state: State<'_, AppState>) -> Result<VersionInfo, String> {
    let client_lock = state.client.read().await;
    let client = client_lock
        .as_ref()
        .ok_or("Not connected to sing-box API")?;
    client.get_version().await
}

#[tauri::command]
pub async fn get_started_at(state: State<'_, AppState>) -> Result<i64, String> {
    let client_lock = state.client.read().await;
    let client = client_lock
        .as_ref()
        .ok_or("Not connected to sing-box API")?;
    client.get_started_at().await
}

#[tauri::command]
pub async fn get_service_status(state: State<'_, AppState>) -> Result<ServiceStatusInfo, String> {
    let client_lock = state.client.read().await;
    let client = client_lock
        .as_ref()
        .ok_or("Not connected to sing-box API")?;
    let statuses = client.subscribe_service_status().await?;
    statuses
        .into_iter()
        .last()
        .ok_or_else(|| "No service status received".to_string())
}

#[tauri::command]
pub async fn get_clash_mode_status(state: State<'_, AppState>) -> Result<ClashModeInfo, String> {
    let client_lock = state.client.read().await;
    let client = client_lock
        .as_ref()
        .ok_or("Not connected to sing-box API")?;
    client.get_clash_mode_status().await
}

#[tauri::command]
pub async fn set_clash_mode(mode: String, state: State<'_, AppState>) -> Result<(), String> {
    let client_lock = state.client.read().await;
    let client = client_lock
        .as_ref()
        .ok_or("Not connected to sing-box API")?;
    client.set_clash_mode(&mode).await
}
