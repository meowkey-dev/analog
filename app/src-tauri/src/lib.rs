//! The desktop shell.
//!
//! Deliberately thin. The window loads the same bundle the server serves, and that
//! bundle already knows how to be told which server to talk to (web/src/connection.ts)
//! — so there is no Rust-side API client, no second copy of the auth rules, and
//! nothing here that can drift from the contract.
//!
//! The one thing this cannot do is inherit an origin, which is why the connection
//! screen exists: a browser served by the server knows where home is, and this
//! does not.

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .run(tauri::generate_context!())
        .expect("error while running analog");
}
