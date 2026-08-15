use tauri::State;

use crate::state::{AppState, GroupInfo};

#[tauri::command]
pub async fn get_groups(state: State<'_, AppState>) -> Result<Vec<GroupInfo>, String> {
    let client_lock = state.client.read().await;
    let client = client_lock
        .as_ref()
        .ok_or("Not connected to sing-box API")?;
    client.subscribe_groups().await
}

#[tauri::command]
pub async fn select_outbound(
    group_tag: String,
    outbound_tag: String,
    state: State<'_, AppState>,
) -> Result<(), String> {
    let client_lock = state.client.read().await;
    let client = client_lock
        .as_ref()
        .ok_or("Not connected to sing-box API")?;
    client.select_outbound(&group_tag, &outbound_tag).await
}

#[tauri::command]
pub async fn url_test(
    outbound_tag: String,
    state: State<'_, AppState>,
) -> Result<(), String> {
    let client_lock = state.client.read().await;
    let client = client_lock
        .as_ref()
        .ok_or("Not connected to sing-box API")?;
    client.url_test(&outbound_tag).await
}

#[tauri::command]
pub async fn set_group_expand(
    group_tag: String,
    is_expand: bool,
    state: State<'_, AppState>,
) -> Result<(), String> {
    let client_lock = state.client.read().await;
    let client = client_lock
        .as_ref()
        .ok_or("Not connected to sing-box API")?;
    client.set_group_expand(&group_tag, is_expand).await
}