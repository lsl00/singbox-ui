mod commands;
mod grpc;
mod state;
mod tray;

use std::sync::atomic::Ordering;
use std::time::Duration;

use state::AppState;
use tauri::{Emitter, Manager};

#[derive(Clone, serde::Serialize)]
struct ConnectionState {
    connected: bool,
    error: Option<String>,
}

fn spawn_status_monitor(app: tauri::AppHandle) {
    tauri::async_runtime::spawn(async move {
        let mut was_connected = false;

        loop {
            let client = {
                let state = app.state::<AppState>();
                let guard = state.client.read().await;
                let client = guard.clone();
                drop(guard);
                client
            };

            if let Some(client) = client {
                match client.subscribe_status().await {
                    Ok(statuses) => {
                        if let Some(latest) = statuses.into_iter().last() {
                            let info = state::StatusInfo::from(latest);
                            let _ = app.emit("status-update", &info);
                            let title = format!(
                                "↑{} ↓{}",
                                tray::format_rate(info.uplink),
                                tray::format_rate(info.downlink)
                            );
                            tray::update_tray_title(&app, &title);

                            if !was_connected {
                                was_connected = true;
                                let _ = app.emit(
                                    "connection-state",
                                    ConnectionState {
                                        connected: true,
                                        error: None,
                                    },
                                );
                            }
                        }
                    }
                    Err(e) => {
                        log::warn!("status poll failed: {}", e);
                        tray::update_tray_title(&app, "sing-box");
                        if was_connected {
                            was_connected = false;
                            let _ = app.emit(
                                "connection-state",
                                ConnectionState {
                                    connected: false,
                                    error: Some(e),
                                },
                            );
                        }
                    }
                }
            } else if was_connected {
                was_connected = false;
                tray::update_tray_title(&app, "sing-box");
                let _ = app.emit(
                    "connection-state",
                    ConnectionState {
                        connected: false,
                        error: None,
                    },
                );
            }

            tokio::time::sleep(Duration::from_millis(500)).await;
        }
    });
}

fn spawn_tray_proxy_monitor(app: tauri::AppHandle) {
    tauri::async_runtime::spawn(async move {
        let mut last_signature: Option<String> = None;

        loop {
            let client = {
                let state = app.state::<AppState>();
                let guard = state.client.read().await;
                let client = guard.clone();
                drop(guard);
                client
            };

            if let Some(client) = client {
                match client.subscribe_groups().await {
                    Ok(groups) => {
                        let signature = serde_json::to_string(&groups).unwrap_or_default();
                        if last_signature.as_ref() != Some(&signature) {
                            last_signature = Some(signature);
                            tray::update_tray_menu(&app, &groups);
                        }
                    }
                    Err(_) => {}
                }
            } else if last_signature.is_some() {
                last_signature = None;
                tray::update_tray_menu(&app, &[]);
            }

            tokio::time::sleep(Duration::from_secs(3)).await;
        }
    });
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .manage(AppState::new())
        .setup(|app| {
            if cfg!(debug_assertions) {
                app.handle().plugin(
                    tauri_plugin_log::Builder::default()
                        .level(log::LevelFilter::Info)
                        .build(),
                )?;
            }
            tray::setup_tray(app.handle())?;
            spawn_status_monitor(app.handle().clone());
            spawn_tray_proxy_monitor(app.handle().clone());
            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                let app = window.app_handle();
                let quitting = app
                    .try_state::<AppState>()
                    .map(|s| s.quitting.load(Ordering::SeqCst))
                    .unwrap_or(false);
                if !quitting {
                    api.prevent_close();
                    let _ = window.hide();
                }
            }
        })
        .invoke_handler(tauri::generate_handler![
            commands::status::connect,
            commands::status::disconnect,
            commands::status::get_status,
            commands::status::get_version,
            commands::status::get_started_at,
            commands::status::get_service_status,
            commands::status::get_clash_mode_status,
            commands::status::set_clash_mode,
            commands::groups::get_groups,
            commands::groups::select_outbound,
            commands::groups::url_test,
            commands::groups::set_group_expand,
            commands::connections::get_connections,
            commands::connections::close_connection,
            commands::connections::close_all_connections,
            commands::logs::get_logs,
            commands::logs::clear_logs,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
