use std::time::Duration;

use prost::Message;
use tokio_stream::StreamExt;

use crate::state::{
    ApiConfig, ClashModeInfo, ConnectionEventInfo, ConnectionEventsInfo, ConnectionInfo,
    GroupInfo, GroupItemInfo, LogEntryInfo, LogsInfo, ServiceStatusInfo,
    VersionInfo,
};

const MAX_STREAM_BUFFER: usize = 16 * 1024 * 1024;
const MAX_STREAM_MESSAGES: usize = 4096;

#[derive(Debug, Clone)]
pub struct StatusData {
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

pub struct GrpcClient {
    http_client: reqwest::Client,
    base_url: String,
    secret: String,
}

impl GrpcClient {
    pub fn new(config: &ApiConfig) -> Result<Self, String> {
        let http_client = reqwest::Client::builder()
            .no_proxy()
            .connect_timeout(Duration::from_secs(5))
            .timeout(Duration::from_secs(10))
            .build()
            .map_err(|e| format!("Failed to create HTTP client: {}", e))?;
        Ok(Self {
            http_client,
            base_url: config.url.trim_end_matches('/').to_string(),
            secret: config.secret.clone(),
        })
    }

    fn grpc_url(&self, service: &str, method: &str) -> String {
        format!("{}/{}/{}", self.base_url, service, method)
    }

    fn encode_frame(body: &[u8]) -> Vec<u8> {
        let mut frame = vec![0u8; 5 + body.len()];
        frame[1] = (body.len() >> 24) as u8;
        frame[2] = (body.len() >> 16) as u8;
        frame[3] = (body.len() >> 8) as u8;
        frame[4] = body.len() as u8;
        frame[5..].copy_from_slice(body);
        frame
    }

    fn decode_frames(data: &[u8]) -> Result<Vec<Vec<u8>>, String> {
        let mut frames = Vec::new();
        let mut offset = 0;
        while offset + 5 <= data.len() {
            let flag = data[offset];
            let length = ((data[offset + 1] as usize) << 24)
                | ((data[offset + 2] as usize) << 16)
                | ((data[offset + 3] as usize) << 8)
                | (data[offset + 4] as usize);
            if offset + 5 + length > data.len() {
                break;
            }
            if flag & 0x80 == 0 {
                frames.push(data[offset + 5..offset + 5 + length].to_vec());
            } else {
                let trailers = String::from_utf8_lossy(&data[offset + 5..offset + 5 + length]);
                if let Some(status_line) = trailers.lines().find(|l| l.starts_with("grpc-status:")) {
                    let code = status_line
                        .trim_start_matches("grpc-status:")
                        .trim();
                    if code != "0" {
                        let msg = trailers
                            .lines()
                            .find(|l| l.starts_with("grpc-message:"))
                            .map(|l| l.trim_start_matches("grpc-message:").trim().to_string())
                            .unwrap_or_default();
                        return Err(format!("gRPC error code {}: {}", code, msg));
                    }
                }
            }
            offset += 5 + length;
        }
        Ok(frames)
    }

    async fn unary_call<Req: Message, Res: Message + Default>(
        &self,
        service: &str,
        method: &str,
        request: &Req,
    ) -> Result<Res, String> {
        let body = request.encode_to_vec();
        let frame = Self::encode_frame(&body);

        let mut req = self
            .http_client
            .post(self.grpc_url(service, method))
            .header("Content-Type", "application/grpc-web+proto")
            .header("X-Grpc-Web", "1")
            .body(frame);

        if !self.secret.is_empty() {
            req = req.header("Authorization", format!("Bearer {}", self.secret));
        }

        let response = req
            .send()
            .await
            .map_err(|e| format!("gRPC request failed: {}", e))?;

        let status = response.status();
        if !status.is_success() {
            return Err(format!("HTTP error: {}", status));
        }

        let data = response
            .bytes()
            .await
            .map_err(|e| format!("Failed to read response: {}", e))?;

        let frames = Self::decode_frames(&data)?;
        if frames.is_empty() {
            return Err("Empty response".to_string());
        }

        let res = Res::decode(frames[0].as_slice())
            .map_err(|e| format!("Failed to decode response: {}", e))?;
        Ok(res)
    }

    async fn server_stream_call<Req: Message, Res: Message + Default>(
        &self,
        service: &str,
        method: &str,
        request: &Req,
        timeout: Option<Duration>,
    ) -> Result<Vec<Res>, String> {
        let body = request.encode_to_vec();
        let frame = Self::encode_frame(&body);

        let mut req = self
            .http_client
            .post(self.grpc_url(service, method))
            .header("Content-Type", "application/grpc-web+proto")
            .header("X-Grpc-Web", "1")
            .body(frame);

        if !self.secret.is_empty() {
            req = req.header("Authorization", format!("Bearer {}", self.secret));
        }

        let response = req
            .send()
            .await
            .map_err(|e| format!("gRPC request failed: {}", e))?;

        let status = response.status();
        if !status.is_success() {
            return Err(format!("HTTP error: {}", status));
        }

        let mut stream = response.bytes_stream();
        let mut buffer: Vec<u8> = Vec::new();
        let mut results: Vec<Res> = Vec::new();

        let deadline = timeout.map(|t| std::time::Instant::now() + t);

        loop {
            let remaining = deadline.map(|d| d.saturating_duration_since(std::time::Instant::now()));

            let chunk = match remaining {
                Some(r) if r.is_zero() => break,
                Some(r) => match tokio::time::timeout(r, stream.next()).await {
                    Ok(Some(Ok(chunk))) => chunk,
                    Ok(Some(Err(e))) => return Err(format!("Stream error: {}", e)),
                    Ok(None) => break,
                    Err(_) => break,
                },
                None => match stream.next().await {
                    Some(Ok(chunk)) => chunk,
                    Some(Err(e)) => return Err(format!("Stream error: {}", e)),
                    None => break,
                },
            };

            buffer.extend_from_slice(&chunk);
            if buffer.len() > MAX_STREAM_BUFFER {
                return Err("gRPC stream buffer exceeded 16 MiB".to_string());
            }

            let mut offset = 0;
            while offset + 5 <= buffer.len() {
                let flag = buffer[offset];
                let length = ((buffer[offset + 1] as usize) << 24)
                    | ((buffer[offset + 2] as usize) << 16)
                    | ((buffer[offset + 3] as usize) << 8)
                    | (buffer[offset + 4] as usize);
                if offset + 5 + length > buffer.len() {
                    break;
                }
                if flag & 0x80 == 0 {
                    let message = Res::decode(&buffer[offset + 5..offset + 5 + length])
                        .map_err(|e| format!("Failed to decode response: {}", e))?;
                    results.push(message);
                    if results.len() > MAX_STREAM_MESSAGES {
                        return Err("gRPC stream contained too many messages".to_string());
                    }
                } else {
                    let trailers =
                        String::from_utf8_lossy(&buffer[offset + 5..offset + 5 + length]);
                    if let Some(status_line) =
                        trailers.lines().find(|l| l.starts_with("grpc-status:"))
                    {
                        let code = status_line.trim_start_matches("grpc-status:").trim();
                        if code != "0" {
                            let msg = trailers
                                .lines()
                                .find(|l| l.starts_with("grpc-message:"))
                                .map(|l| l.trim_start_matches("grpc-message:").trim().to_string())
                                .unwrap_or_default();
                            return Err(format!("gRPC error code {}: {}", code, msg));
                        }
                    }
                }
                offset += 5 + length;
            }
            buffer.drain(..offset);
        }

        Ok(results)
    }

    pub async fn get_version(&self) -> Result<VersionInfo, String> {
        let response: crate::grpc::proto::Version = self
            .unary_call(
                "daemon.StartedService",
                "GetVersion",
                &(),
            )
            .await?;
        Ok(VersionInfo {
            version: response.version,
            api_version: response.api_version,
        })
    }

    pub async fn subscribe_service_status(
        &self,
    ) -> Result<Vec<ServiceStatusInfo>, String> {
        let responses: Vec<crate::grpc::proto::ServiceStatus> = self
            .server_stream_call(
                "daemon.StartedService",
                "SubscribeServiceStatus",
                &(),
                Some(Duration::from_millis(1000)),
            )
            .await?;
        Ok(responses
            .into_iter()
            .map(|s| ServiceStatusInfo {
                status: s.status,
                error_message: s.error_message,
            })
            .collect())
    }

    pub async fn subscribe_status(&self) -> Result<Vec<StatusData>, String> {
        let request = crate::grpc::proto::SubscribeStatusRequest {
            interval: 1_000_000_000,
        };
        let responses: Vec<crate::grpc::proto::Status> = self
            .server_stream_call(
                "daemon.StartedService",
                "SubscribeStatus",
                &request,
                Some(Duration::from_millis(1500)),
            )
            .await?;
        Ok(responses
            .into_iter()
            .map(|s| StatusData {
                memory: s.memory,
                goroutines: s.goroutines,
                connections_in: s.connections_in,
                connections_out: s.connections_out,
                traffic_available: s.traffic_available,
                uplink: s.uplink,
                downlink: s.downlink,
                uplink_total: s.uplink_total,
                downlink_total: s.downlink_total,
            })
            .collect())
    }

    pub async fn subscribe_groups(&self) -> Result<Vec<GroupInfo>, String> {
        let responses: Vec<crate::grpc::proto::Groups> = self
            .server_stream_call(
                "daemon.StartedService",
                "SubscribeGroups",
                &(),
                Some(Duration::from_millis(1000)),
            )
            .await?;
        let mut groups = Vec::new();
        for r in responses {
            for g in r.group {
                groups.push(GroupInfo {
                    tag: g.tag,
                    group_type: g.r#type,
                    selectable: g.selectable,
                    selected: g.selected,
                    is_expand: g.is_expand,
                    items: g
                        .items
                        .into_iter()
                        .map(|i| GroupItemInfo {
                            tag: i.tag,
                            item_type: i.r#type,
                            url_test_time: i.url_test_time,
                            url_test_delay: i.url_test_delay,
                        })
                        .collect(),
                });
            }
        }
        Ok(groups)
    }

    pub async fn subscribe_connections(&self) -> Result<Vec<ConnectionEventsInfo>, String> {
        let request = crate::grpc::proto::SubscribeConnectionsRequest {
            interval: 1_000_000_000,
        };
        let responses: Vec<crate::grpc::proto::ConnectionEvents> = self
            .server_stream_call(
                "daemon.StartedService",
                "SubscribeConnections",
                &request,
                Some(Duration::from_millis(1000)),
            )
            .await?;
        Ok(responses
            .into_iter()
            .map(|r| ConnectionEventsInfo {
                events: r
                    .events
                    .into_iter()
                    .map(|e| {
                        let conn = e.connection.map(|c| ConnectionInfo {
                            id: c.id,
                            inbound: c.inbound,
                            inbound_type: c.inbound_type,
                            ip_version: c.ip_version,
                            network: c.network,
                            source: c.source,
                            destination: c.destination,
                            domain: c.domain,
                            protocol: c.protocol,
                            user: c.user,
                            from_outbound: c.from_outbound,
                            created_at: c.created_at,
                            closed_at: c.closed_at,
                            uplink: c.uplink,
                            downlink: c.downlink,
                            uplink_total: c.uplink_total,
                            downlink_total: c.downlink_total,
                            rule: c.rule,
                            outbound: c.outbound,
                            outbound_type: c.outbound_type,
                            chain_list: c.chain_list,
                        });
                        ConnectionEventInfo {
                            event_type: e.r#type,
                            id: e.id,
                            connection: conn,
                            uplink_delta: e.uplink_delta,
                            downlink_delta: e.downlink_delta,
                            closed_at: e.closed_at,
                        }
                    })
                    .collect(),
                reset: r.reset,
            })
            .collect())
    }

    pub async fn subscribe_log(&self) -> Result<Vec<LogsInfo>, String> {
        let responses: Vec<crate::grpc::proto::Log> = self
            .server_stream_call(
                "daemon.StartedService",
                "SubscribeLog",
                &(),
                Some(Duration::from_millis(1000)),
            )
            .await?;
        Ok(responses
            .into_iter()
            .map(|r| LogsInfo {
                messages: r
                    .messages
                    .into_iter()
                    .map(|m| LogEntryInfo {
                        level: m.level,
                        message: m.message,
                    })
                    .collect(),
                reset: r.reset,
            })
            .collect())
    }

    pub async fn clear_logs(&self) -> Result<(), String> {
        self.unary_call::<(), ()>(
            "daemon.StartedService",
            "ClearLogs",
            &(),
        )
        .await?;
        Ok(())
    }

    pub async fn get_started_at(&self) -> Result<i64, String> {
        let response: crate::grpc::proto::StartedAt = self
            .unary_call(
                "daemon.StartedService",
                "GetStartedAt",
                &(),
            )
            .await?;
        Ok(response.started_at)
    }

    pub async fn select_outbound(
        &self,
        group_tag: &str,
        outbound_tag: &str,
    ) -> Result<(), String> {
        let request = crate::grpc::proto::SelectOutboundRequest {
            group_tag: group_tag.to_string(),
            outbound_tag: outbound_tag.to_string(),
        };
        self.unary_call::<crate::grpc::proto::SelectOutboundRequest, ()>(
            "daemon.StartedService",
            "SelectOutbound",
            &request,
        )
        .await?;
        Ok(())
    }

    pub async fn url_test(&self, outbound_tag: &str) -> Result<(), String> {
        let request = crate::grpc::proto::UrlTestRequest {
            outbound_tag: outbound_tag.to_string(),
        };
        self.unary_call::<crate::grpc::proto::UrlTestRequest, ()>(
            "daemon.StartedService",
            "URLTest",
            &request,
        )
        .await?;
        Ok(())
    }

    pub async fn set_group_expand(
        &self,
        group_tag: &str,
        is_expand: bool,
    ) -> Result<(), String> {
        let request = crate::grpc::proto::SetGroupExpandRequest {
            group_tag: group_tag.to_string(),
            is_expand,
        };
        self.unary_call::<crate::grpc::proto::SetGroupExpandRequest, ()>(
            "daemon.StartedService",
            "SetGroupExpand",
            &request,
        )
        .await?;
        Ok(())
    }

    pub async fn close_connection(&self, id: &str) -> Result<(), String> {
        let request = crate::grpc::proto::CloseConnectionRequest {
            id: id.to_string(),
        };
        self.unary_call::<crate::grpc::proto::CloseConnectionRequest, ()>(
            "daemon.StartedService",
            "CloseConnection",
            &request,
        )
        .await?;
        Ok(())
    }

    pub async fn close_all_connections(&self) -> Result<(), String> {
        self.unary_call::<(), ()>(
            "daemon.StartedService",
            "CloseAllConnections",
            &(),
        )
        .await?;
        Ok(())
    }

    pub async fn get_clash_mode_status(&self) -> Result<ClashModeInfo, String> {
        let response: crate::grpc::proto::ClashModeStatus = self
            .unary_call(
                "daemon.StartedService",
                "GetClashModeStatus",
                &(),
            )
            .await?;
        Ok(ClashModeInfo {
            mode_list: response.mode_list,
            current_mode: response.current_mode,
        })
    }

    pub async fn set_clash_mode(&self, mode: &str) -> Result<(), String> {
        let request = crate::grpc::proto::ClashMode {
            mode: mode.to_string(),
        };
        self.unary_call::<crate::grpc::proto::ClashMode, ()>(
            "daemon.StartedService",
            "SetClashMode",
            &request,
        )
        .await?;
        Ok(())
    }

}
