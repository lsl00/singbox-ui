use tauri::State;

use crate::state::{AppState, ConnectionEventsInfo};

#[tauri::command]
pub async fn get_connections(state: State<'_, AppState>) -> Result<Vec<ConnectionEventsInfo>, String> {
    let client_lock = state.client.read().await;
    let client = client_lock
        .as_ref()
        .ok_or("Not connected to sing-box API")?;
    client.subscribe_connections().await
}

#[tauri::command]
pub async fn close_connection(id: String, state: State<'_, AppState>) -> Result<(), String> {
    let client_lock = state.client.read().await;
    let client = client_lock
        .as_ref()
        .ok_or("Not connected to sing-box API")?;
    client.close_connection(&id).await
}

#[tauri::command]
pub async fn close_all_connections(state: State<'_, AppState>) -> Result<(), String> {
    let client_lock = state.client.read().await;
    let client = client_lock
        .as_ref()
        .ok_or("Not connected to sing-box API")?;
    client.close_all_connections().await
}