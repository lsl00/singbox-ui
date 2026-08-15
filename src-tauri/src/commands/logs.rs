use tauri::State;

use crate::state::{AppState, LogsInfo};

#[tauri::command]
pub async fn get_logs(state: State<'_, AppState>) -> Result<Vec<LogsInfo>, String> {
    let client_lock = state.client.read().await;
    let client = client_lock
        .as_ref()
        .ok_or("Not connected to sing-box API")?;
    client.subscribe_log().await
}

#[tauri::command]
pub async fn clear_logs(state: State<'_, AppState>) -> Result<(), String> {
    let client_lock = state.client.read().await;
    let client = client_lock
        .as_ref()
        .ok_or("Not connected to sing-box API")?;
    client.clear_logs().await
}