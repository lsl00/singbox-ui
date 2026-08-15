pub mod client;

pub(crate) mod proto {
    tonic::include_proto!("daemon");
}