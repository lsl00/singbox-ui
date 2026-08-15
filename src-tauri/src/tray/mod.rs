use std::sync::atomic::Ordering;

use tauri::{
    menu::{CheckMenuItemBuilder, MenuBuilder, MenuItemBuilder, SubmenuBuilder},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    AppHandle, Manager,
};

use crate::state::{AppState, GroupInfo};

const PROXY_PREFIX: &str = "proxy:";

pub fn update_tray_title(app: &AppHandle, title: &str) {
    if let Some(tray) = app.tray_by_id("main-tray") {
        let _ = tray.set_title(Some(title));
    }
}

pub fn format_rate(bytes_per_second: i64) -> String {
    if bytes_per_second <= 0 {
        return "0".to_string();
    }
    let units = ["B", "K", "M", "G", "T"];
    let mut value = bytes_per_second as f64;
    let mut unit = 0;
    while value >= 1024.0 && unit < units.len() - 1 {
        value /= 1024.0;
        unit += 1;
    }
    if value >= 100.0 {
        format!("{:.0}{}", value, units[unit])
    } else if value >= 10.0 {
        format!("{:.1}{}", value, units[unit])
    } else {
        format!("{:.2}{}", value, units[unit])
    }
}

fn format_delay(delay: i32) -> String {
    if delay <= 0 {
        "Timeout".to_string()
    } else {
        format!("{}ms", delay)
    }
}

fn build_menu(
    app: &AppHandle,
    groups: &[GroupInfo],
) -> Result<tauri::menu::Menu<tauri::Wry>, Box<dyn std::error::Error>> {
    let show_item = MenuItemBuilder::with_id("show", "Show Dashboard").build(app)?;
    let hide_item = MenuItemBuilder::with_id("hide", "Hide Dashboard").build(app)?;
    let quit_item = MenuItemBuilder::with_id("quit", "Quit").build(app)?;

    let mut builder = MenuBuilder::new(app)
        .item(&show_item)
        .item(&hide_item)
        .separator();

    let selectable: Vec<&GroupInfo> = groups
        .iter()
        .filter(|g| g.selectable && !g.items.is_empty())
        .collect();

    if !selectable.is_empty() {
        let mut proxy_submenu = SubmenuBuilder::new(app, "Proxy");
        for group in &selectable {
            let mut group_submenu = SubmenuBuilder::new(app, group.tag.as_str());
            for item in &group.items {
                let selected = item.tag == group.selected;
                let label = if item.url_test_delay > 0 {
                    format!("{}  ({})", item.tag, format_delay(item.url_test_delay))
                } else {
                    item.tag.clone()
                };
                let id = format!("{}{}:{}", PROXY_PREFIX, group.tag, item.tag);
                let menu_item = CheckMenuItemBuilder::with_id(id, label)
                    .checked(selected)
                    .build(app)?;
                group_submenu = group_submenu.item(&menu_item);
            }
            proxy_submenu = proxy_submenu.item(&group_submenu.build()?);
        }
        builder = builder.item(&proxy_submenu.build()?).separator();
    }

    builder = builder.item(&quit_item);
    Ok(builder.build()?)
}

pub fn update_tray_menu(app: &AppHandle, groups: &[GroupInfo]) {
    if let Some(tray) = app.tray_by_id("main-tray") {
        if let Ok(menu) = build_menu(app, groups) {
            let _ = tray.set_menu(Some(menu));
        }
    }
}

fn handle_proxy_select(app: &AppHandle, id: &str) {
    let rest = match id.strip_prefix(PROXY_PREFIX) {
        Some(r) => r,
        None => return,
    };
    let (group, node) = match rest.split_once(':') {
        Some(p) => p,
        None => return,
    };
    let group = group.to_string();
    let node = node.to_string();
    let handle = app.clone();
    tauri::async_runtime::spawn(async move {
        let state = handle.state::<AppState>();
        let client = state.client.read().await.clone();
        if let Some(client) = client {
            if let Err(e) = client.select_outbound(&group, &node).await {
                log::warn!("tray select outbound failed: {}", e);
            }
        }
    });
}

pub fn setup_tray(app: &AppHandle) -> Result<(), Box<dyn std::error::Error>> {
    let menu = build_menu(app, &[])?;

    let _tray = TrayIconBuilder::with_id("main-tray")
        .icon(tauri::image::Image::from_bytes(include_bytes!(
            "../../icons/tray-icon.png"
        ))?)
        .icon_as_template(true)
        .menu(&menu)
        .tooltip("sing-box Dashboard")
        .on_menu_event(|app, event| {
            let id = event.id().as_ref().to_string();
            match id.as_str() {
                "show" => {
                    if let Some(window) = app.get_webview_window("main") {
                        let _ = window.show();
                        let _ = window.set_focus();
                    }
                }
                "hide" => {
                    if let Some(window) = app.get_webview_window("main") {
                        let _ = window.hide();
                    }
                }
                "quit" => {
                    if let Some(state) = app.try_state::<AppState>() {
                        state.quitting.store(true, Ordering::SeqCst);
                    }
                    app.exit(0);
                }
                _ => {
                    if id.starts_with(PROXY_PREFIX) {
                        handle_proxy_select(app, &id);
                    }
                }
            }
        })
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                let app = tray.app_handle();
                if let Some(window) = app.get_webview_window("main") {
                    if window.is_visible().unwrap_or(false) {
                        let _ = window.hide();
                    } else {
                        let _ = window.show();
                        let _ = window.set_focus();
                    }
                }
            }
        })
        .build(app)?;

    Ok(())
}
