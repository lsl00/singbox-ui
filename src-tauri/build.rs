fn main() {
    tauri_build::build();
    tonic_build::configure()
        .build_server(false)
        .build_client(false)
        .compile_protos(
            &["proto/started_service.proto"],
            &["proto/"],
        )
        .expect("Failed to compile protobuf");
}
