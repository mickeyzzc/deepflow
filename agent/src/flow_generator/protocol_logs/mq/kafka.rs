/*
 * Copyright (c) 2023 Yunshan Networks
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
use serde::Serialize;

use crate::{
    common::{
        enums::IpProtocol,
        flow::{L7PerfStats, L7Protocol, PacketDirection},
        l7_protocol_info::{L7ProtocolInfo, L7ProtocolInfoInterface},
        l7_protocol_log::{KafkaInfoCache, L7ParseResult, L7ProtocolParserInterface, ParseParam},
    },
    flow_generator::{
        error::{Error, Result},
        protocol_logs::{
            consts::{KAFKA_REQ_HEADER_LEN, KAFKA_RESP_HEADER_LEN},
            pb_adapter::{ExtendedInfo, L7ProtocolSendLog, L7Request, L7Response},
            value_is_default, value_is_negative, AppProtoHead, L7ResponseStatus, LogMessageType,
        },
    },
    utils::bytes::{read_i16_be, read_u16_be, read_u32_be},
};
use encoding_rs::UTF_16BE;

const KAFKA_PRODUCE: u16 = 0;
const KAFKA_FETCH: u16 = 1;

#[derive(Serialize, Debug, Default, Clone)]
pub struct KafkaInfo {
    msg_type: LogMessageType,
    #[serde(skip)]
    is_tls: bool,

    #[serde(rename = "request_id", skip_serializing_if = "value_is_default")]
    pub correlation_id: u32,

    // request
    #[serde(rename = "request_length", skip_serializing_if = "value_is_negative")]
    pub req_msg_size: Option<u32>,
    #[serde(skip)]
    pub api_version: u16,
    #[serde(rename = "request_type")]
    pub api_key: u16,
    #[serde(skip)]
    pub client_id: String,
    #[serde(skip)]
    pub topics: Option<String>,

    // reponse
    #[serde(rename = "response_length", skip_serializing_if = "value_is_negative")]
    pub resp_msg_size: Option<u32>,
    #[serde(rename = "response_status")]
    pub status: L7ResponseStatus,
    #[serde(rename = "response_code", skip_serializing_if = "Option::is_none")]
    pub status_code: Option<i16>,

    rrt: u64,
}

impl L7ProtocolInfoInterface for KafkaInfo {
    fn session_id(&self) -> Option<u32> {
        Some(self.correlation_id)
    }

    fn merge_log(&mut self, other: crate::common::l7_protocol_info::L7ProtocolInfo) -> Result<()> {
        if let L7ProtocolInfo::KafkaInfo(other) = other {
            self.merge(other);
        }
        Ok(())
    }

    fn app_proto_head(&self) -> Option<AppProtoHead> {
        Some(AppProtoHead {
            proto: L7Protocol::Kafka,
            msg_type: self.msg_type,
            rrt: self.rrt,
        })
    }

    fn is_tls(&self) -> bool {
        self.is_tls
    }
}

impl KafkaInfo {
    // https://kafka.apache.org/protocol.html
    const API_KEY_MAX: u16 = 67;
    pub fn merge(&mut self, other: Self) {
        if self.resp_msg_size.is_none() {
            self.resp_msg_size = other.resp_msg_size;
        }
        if other.status != L7ResponseStatus::default() {
            self.status = other.status;
        }
        if other.status_code != None {
            self.status_code = other.status_code;
        }
        if other.topics != None {
            self.topics = other.topics;
        }
    }

    pub fn check(&self) -> bool {
        if self.api_key > Self::API_KEY_MAX {
            return false;
        }
        return self.client_id.len() > 0 && self.client_id.is_ascii();
    }

    pub fn get_command(&self) -> &'static str {
        let command_str = [
            "Produce",
            "Fetch",
            "ListOffsets",
            "Metadata",
            "LeaderAndIsr",
            "StopReplica",
            "UpdateMetadata",
            "ControlledShutdown",
            "OffsetCommit",
            "OffsetFetch",
            // 10
            "FindCoordinator",
            "JoinGroup",
            "Heartbeat",
            "LeaveGroup",
            "SyncGroup",
            "DescribeGroups",
            "ListGroups",
            "SaslHandshake",
            "ApiVersions",
            "CreateTopics",
            // 20
            "DeleteTopics",
            "DeleteRecords",
            "InitProducerId",
            "OffsetForLeaderEpoch",
            "AddPartitionsToTxn",
            "AddOffsetsToTxn",
            "EndTxn",
            "WriteTxnMarkers",
            "TxnOffsetCommit",
            "DescribeAcls",
            // 30
            "CreateAcls",
            "DeleteAcls",
            "DescribeConfigs",
            "AlterConfigs",
            "AlterReplicaLogDirs",
            "DescribeLogDirs",
            "SaslAuthenticate",
            "CreatePartitions",
            "CreateDelegationToken",
            "RenewDelegationToken",
            // 40
            "ExpireDelegationToken",
            "DescribeDelegationToken",
            "DeleteGroups",
            "ElectLeaders",
            "IncrementalAlterConfigs",
            "AlterPartitionReassignments",
            "ListPartitionReassignments",
            "OffsetDelete",
            "DescribeClientQuotas",
            "AlterClientQuotas",
            //50
            "DescribeUserScramCredentials",
            "AlterUserScramCredentials",
            "AlterIsr",
            "UpdateFeatures",
            "DescribeCluster",
            "DescribeProducers",
            "DescribeTransactions",
            "ListTransactions",
            "AllocateProducerIds",
        ];
        match self.api_key {
            0..=58 => command_str[self.api_key as usize],
            _ => "",
        }
    }
}

impl From<KafkaInfo> for L7ProtocolSendLog {
    fn from(f: KafkaInfo) -> Self {
        let command_str = f.get_command();
        let str_version = f.api_version.to_string();
        let log = L7ProtocolSendLog {
            req_len: f.req_msg_size,
            resp_len: f.resp_msg_size,
            req: L7Request {
                req_type: String::from(command_str) + "_v" + str_version.as_str(),
                resource: f.topics.unwrap_or_default(),
                ..Default::default()
            },
            resp: L7Response {
                status: f.status,
                code: Some(f.status_code.unwrap_or(0).into()),
                ..Default::default()
            },
            ext_info: Some(ExtendedInfo {
                request_id: Some(f.correlation_id),
                ..Default::default()
            }),
            ..Default::default()
        };
        return log;
    }
}

#[derive(Clone, Serialize, Default)]
pub struct KafkaLog {
    #[serde(skip)]
    perf_stats: Option<L7PerfStats>,
}

impl L7ProtocolParserInterface for KafkaLog {
    fn check_payload(&mut self, payload: &[u8], param: &ParseParam) -> bool {
        if !param.ebpf_type.is_raw_protocol()
            || param.l4_protocol != IpProtocol::TCP
            || payload.len() < KAFKA_REQ_HEADER_LEN
        {
            return false;
        }
        let mut info = KafkaInfo::default();
        let ok = self.request(payload, true, &mut info).is_ok() && info.check();
        self.reset();
        ok
    }

    fn parse_payload(&mut self, payload: &[u8], param: &ParseParam) -> Result<L7ParseResult> {
        if self.perf_stats.is_none() && param.parse_perf {
            self.perf_stats = Some(L7PerfStats::default())
        };
        let mut info = KafkaInfo::default();
        Self::parse(self, payload, param.l4_protocol, param.direction, &mut info)?;

        // handle kafka status code
        {
            let mut log_cache = param.l7_perf_cache.borrow_mut();
            if let Some(previous) = log_cache.rrt_cache.get(&info.cal_cache_key(param)) {
                match (previous.msg_type, info.msg_type) {
                    (LogMessageType::Request, LogMessageType::Response)
                        if param.time < previous.time + param.rrt_timeout as u64 =>
                    {
                        if let Some(req) = previous.kafka_info.as_ref() {
                            self.set_status_code(
                                req.api_key,
                                req.api_version,
                                &payload[KAFKA_RESP_HEADER_LEN..],
                                &mut info,
                                None,
                            )
                        }
                    }
                    (LogMessageType::Response, LogMessageType::Request)
                        if previous.time < param.time + param.rrt_timeout as u64 =>
                    {
                        if let Some(resp) = previous.kafka_info.as_ref() {
                            self.set_status_code(
                                info.api_key,
                                info.api_version,
                                &payload[KAFKA_REQ_HEADER_LEN..],
                                &mut info,
                                Some(resp.code),
                            )
                        }
                    }
                    _ => {}
                }
            }
        }

        info.cal_rrt(
            param,
            Some(KafkaInfoCache {
                api_key: info.api_key,
                api_version: info.api_version,
                code: info.status_code.unwrap_or(0),
            }),
        )
        .map(|rrt| {
            info.rrt = rrt;
            self.perf_stats.as_mut().map(|p| p.update_rrt(rrt));
        });
        if param.parse_log {
            Ok(L7ParseResult::Single(L7ProtocolInfo::KafkaInfo(info)))
        } else {
            Ok(L7ParseResult::None)
        }
    }

    fn protocol(&self) -> L7Protocol {
        L7Protocol::Kafka
    }

    fn parsable_on_udp(&self) -> bool {
        false
    }

    fn perf_stats(&mut self) -> Option<L7PerfStats> {
        self.perf_stats.take()
    }
}

// Kafka每个api key的不同版本有不同的固定偏移，这里返回固定的偏移。
// 因版本太多且差异大，这里只实现最常见的produce和fetch两个api key。
macro_rules! kafka_apiversion_topic_fixed_offset {
    ($api_key:expr, $api_version:expr) => {
        match $api_key {
            KAFKA_PRODUCE => {
                if $api_version <= 2 {
                    // Offset for API version <= 2
                    10
                } else if $api_version <= 9 {
                    // Offset for API version <= 9
                    12
                } else {
                    // Invalid API version
                    usize::max_value()
                }
            }
            KAFKA_FETCH => {
                if $api_version <= 2 {
                    // Offset for API version <= 2
                    16
                } else if $api_version == 3 {
                    // Offset for API version == 3
                    20
                } else if $api_version <= 6 {
                    // Offset for API version <= 6
                    21
                } else if $api_version <= 12 {
                    // Offset for API version <= 12
                    29
                } else {
                    // Invalid API version
                    usize::max_value()
                }
            }
            _ => usize::max_value(),
        }
    };
}

macro_rules! kafka_apiversion_errcode_fixed_offset {
    ($api_key:expr, $api_version:expr) => {
        match $api_key {
            KAFKA_PRODUCE => {
                if $api_version <= 8 {
                    // Offset for API version <= 2
                    14
                } else if $api_version <= 9 {
                    // Offset for API version <= 9
                    // TODO:
                    usize::max_value()
                } else {
                    // Invalid API version
                    usize::max_value()
                }
            }
            KAFKA_FETCH => {
                if $api_version == 0 {
                    14
                } else if $api_version <= 6 {
                    // Offset for API version <= 6
                    18
                } else if $api_version <= 15 {
                    // Offset for API version in [7..15]
                    4
                } else {
                    // Invalid API version
                    usize::max_value()
                }
            }
            _ => usize::max_value(),
        }
    };
}

impl KafkaLog {
    const MSG_LEN_SIZE: usize = 4;

    // 协议识别的时候严格检查避免误识别，日志解析的时候不用严格检查因为可能有长度截断
    // ================================================================================
    // The protocol identification is strictly checked to avoid misidentification.
    // The log analysis is not strictly checked because there may be length truncation
    fn request(&mut self, payload: &[u8], strict: bool, info: &mut KafkaInfo) -> Result<()> {
        let req_len = read_u32_be(payload);
        info.req_msg_size = Some(req_len);
        let client_id_len = read_u16_be(&payload[12..]) as usize;
        if payload.len() < KAFKA_REQ_HEADER_LEN + client_id_len {
            return Err(Error::KafkaLogParseFailed);
        }

        if strict && req_len as usize != payload.len() - Self::MSG_LEN_SIZE {
            return Err(Error::KafkaLogParseFailed);
        }

        info.msg_type = LogMessageType::Request;
        info.api_key = read_u16_be(&payload[4..]);
        info.api_version = read_u16_be(&payload[6..]);
        info.correlation_id = read_u32_be(&payload[8..]);
        info.client_id = String::from_utf8_lossy(&payload[14..14 + client_id_len]).into_owned();

        if !info.client_id.is_ascii() {
            return Err(Error::KafkaLogParseFailed);
        }

        info.topics = std::option::Option::<String>::from(self.get_topics_name(
            info.api_key,
            info.api_version,
            &payload[14 + client_id_len..],
        ));

        Ok(())
    }

    fn response(&mut self, payload: &[u8], info: &mut KafkaInfo) -> Result<()> {
        info.resp_msg_size = Some(read_u32_be(payload));
        info.correlation_id = read_u32_be(&payload[4..]);
        info.msg_type = LogMessageType::Response;
        Ok(())
    }

    fn parse(
        &mut self,
        payload: &[u8],
        proto: IpProtocol,
        direction: PacketDirection,
        info: &mut KafkaInfo,
    ) -> Result<()> {
        if proto != IpProtocol::TCP {
            return Err(Error::InvalidIpProtocol);
        }

        match direction {
            PacketDirection::ClientToServer => {
                if payload.len() < KAFKA_REQ_HEADER_LEN {
                    return Err(Error::KafkaLogParseFailed);
                }
                self.request(payload, false, info)?;
                self.perf_stats.as_mut().map(|p| p.inc_req());
            }
            PacketDirection::ServerToClient => {
                if payload.len() < KAFKA_RESP_HEADER_LEN {
                    return Err(Error::KafkaLogParseFailed);
                }
                self.response(payload, info)?;
                self.perf_stats.as_mut().map(|p| p.inc_resp());
            }
        }
        Ok(())
    }

    fn get_topics_name(
        &mut self,
        api_key: u16,
        api_version: u16,
        payload: &[u8],
    ) -> Option<String> {
        let mut _fixed_offset = kafka_apiversion_topic_fixed_offset!(api_key, api_version);
        if _fixed_offset == usize::max_value() {
            return None;
        }
        match api_key {
            KAFKA_PRODUCE => {
                if api_version >= 3 && api_version <= 8 {
                    let tid_len = read_i16_be(&payload[0..2]);
                    if tid_len > 0 {
                        _fixed_offset = _fixed_offset + tid_len as usize
                    }
                }
                // 版本9是特别的
                if api_version == 9 {
                    let tid_len = payload[0];
                    if tid_len > 0 {
                        _fixed_offset = _fixed_offset + tid_len as usize
                    }
                    let (_, _, result) = UTF_16BE.decode(
                        &payload
                            [_fixed_offset.._fixed_offset + 1 + payload[_fixed_offset] as usize],
                    );
                    if result {
                        return Some(
                            String::from_utf8_lossy(
                                &payload[_fixed_offset
                                    .._fixed_offset + 1 + payload[_fixed_offset] as usize],
                            )
                            .into_owned(),
                        );
                    } else {
                        return None;
                    };
                }
                let len = read_u16_be(&payload[_fixed_offset.._fixed_offset + 2]);
                if _fixed_offset + 2 + len as usize > payload.len() {
                    return None;
                }
                return Some(
                    String::from_utf8_lossy(
                        &payload[_fixed_offset + 2.._fixed_offset + 2 + len as usize],
                    )
                    .into_owned(),
                );
            }
            KAFKA_FETCH => {
                // 版本12是一个过渡版本，前后的解码协议差异更大
                if api_version == 12 {
                    if payload.len() < _fixed_offset + 1 + payload[_fixed_offset] as usize {
                        return None;
                    }
                    let (_, _, result) = UTF_16BE.decode(
                        &payload
                            [_fixed_offset.._fixed_offset + 1 + payload[_fixed_offset] as usize],
                    );
                    if result {
                        return Some(
                            String::from_utf8_lossy(
                                &payload[_fixed_offset
                                    .._fixed_offset + 1 + payload[_fixed_offset] as usize],
                            )
                            .into_owned(),
                        );
                    } else {
                        return None;
                    };
                }
                let len = read_u16_be(&payload[_fixed_offset.._fixed_offset + 2]);
                return Some(
                    String::from_utf8_lossy(
                        &payload[_fixed_offset + 2.._fixed_offset + 2 + len as usize],
                    )
                    .into_owned(),
                );
            }
            _ => {
                return None;
            }
        }
    }

    /*
        reference:  https://kafka.apache.org/protocol.html#protocol_messages

        only fetch api and api version > 7 parse the error code

        Fetch Response (Version: 7) => throttle_time_ms error_code session_id [responses]
            throttle_time_ms => INT32
            error_code => INT16
            ...
    */
    pub fn set_status_code(
        &mut self,
        api_key: u16,
        api_version: u16,
        payload: &[u8],
        info: &mut KafkaInfo,
        code: Option<i16>,
    ) {
        if !code.is_none() {
            if code == Some(0) {
                info.status = L7ResponseStatus::Ok;
            } else {
                info.status = L7ResponseStatus::ServerError;
                self.perf_stats.as_mut().map(|p| p.inc_resp_err());
            }
            return;
        }
        // no code decode
        let mut _fixed_offset = kafka_apiversion_errcode_fixed_offset!(api_key, api_version);
        if _fixed_offset == usize::max_value() {
            return;
        }
        if payload.len() < _fixed_offset {
            return;
        }
        let mut topic_len = 0;
        match api_key {
            KAFKA_PRODUCE => {
                if api_version <= 8 {
                    topic_len = read_i16_be(&payload[4..6]);
                }
                // 版本9是特别的，暂不支持
                if api_version == 9 {
                    return;
                };
            }
            KAFKA_FETCH => {
                if api_version == 0 {
                    topic_len = read_i16_be(&payload[4..6]);
                } else if api_version <= 6 {
                    topic_len = read_i16_be(&payload[10..12]);
                } else if api_version >= 12 {
                    // 版本12是一个过渡版本，前后的解码协议差异更大
                    return;
                }
            }
            _ => {
                return;
            }
        }
        if topic_len > 0 {
            _fixed_offset = _fixed_offset + topic_len as usize
        }
        if _fixed_offset + 2 > payload.len() {
            return;
        }
        info.status_code = Some(
            read_i16_be(&payload[_fixed_offset.._fixed_offset + 2])
                .try_into()
                .unwrap(),
        );
        if info.status_code == Some(0) {
            info.status = L7ResponseStatus::Ok;
        } else {
            info.status = L7ResponseStatus::ServerError;
            self.perf_stats.as_mut().map(|p| p.inc_resp_err());
        }
    }
}

#[cfg(test)]
mod tests {
    use std::path::Path;
    use std::rc::Rc;
    use std::{cell::RefCell, fs};

    use super::*;

    use crate::{
        common::{flow::PacketDirection, l7_protocol_log::L7PerfCache, MetaPacket},
        flow_generator::L7_RRT_CACHE_CAPACITY,
        utils::test::Capture,
    };

    const FILE_DIR: &str = "resources/test/flow_generator/kafka";

    fn run(name: &str) -> String {
        let capture = Capture::load_pcap(Path::new(FILE_DIR).join(name), None);
        let log_cache = Rc::new(RefCell::new(L7PerfCache::new(L7_RRT_CACHE_CAPACITY)));
        let mut packets = capture.as_meta_packets();
        if packets.is_empty() {
            return "".to_string();
        }

        let mut output: String = String::new();
        let first_dst_port = packets[0].lookup_key.dst_port;
        for packet in packets.iter_mut() {
            packet.lookup_key.direction = if packet.lookup_key.dst_port == first_dst_port {
                PacketDirection::ClientToServer
            } else {
                PacketDirection::ServerToClient
            };
            let payload = match packet.get_l4_payload() {
                Some(p) => p,
                None => continue,
            };

            let mut kafka = KafkaLog::default();
            let param = &ParseParam::new(packet as &MetaPacket, log_cache.clone(), true, true);

            let is_kafka = kafka.check_payload(payload, param);
            let info = kafka.parse_payload(payload, param);
            if let Ok(info) = info {
                match info.unwrap_single() {
                    L7ProtocolInfo::KafkaInfo(i) => {
                        output.push_str(&format!("{:?} is_kafka: {}\r\n", i, is_kafka));
                    }
                    _ => unreachable!(),
                }
            } else {
                output.push_str(&format!(
                    "{:?} is_kafka: {}\r\n",
                    KafkaInfo::default(),
                    is_kafka
                ));
            }
        }
        output
    }

    #[test]
    fn check() {
        let files = vec![("kafka.pcap", "kafka.result")];

        for item in files.iter() {
            let expected = fs::read_to_string(&Path::new(FILE_DIR).join(item.1)).unwrap();
            let output = run(item.0);

            if output != expected {
                let output_path = Path::new("actual.txt");
                fs::write(&output_path, &output).unwrap();
                assert!(
                    output == expected,
                    "output different from expected {}, written to {:?}",
                    item.1,
                    output_path
                );
            }
        }
    }

    #[test]
    fn check_perf() {
        let expected = vec![
            (
                "kafka.pcap",
                L7PerfStats {
                    request_count: 1,
                    response_count: 1,
                    err_client_count: 0,
                    err_server_count: 0,
                    err_timeout: 0,
                    rrt_count: 1,
                    rrt_sum: 4941,
                    rrt_max: 4941,
                },
            ),
            (
                "kafka_fetch.pcap",
                L7PerfStats {
                    request_count: 1,
                    response_count: 1,
                    err_client_count: 0,
                    err_server_count: 0,
                    err_timeout: 0,
                    rrt_count: 1,
                    rrt_sum: 504829,
                    rrt_max: 504829,
                },
            ),
        ];

        for item in expected.iter() {
            assert_eq!(item.1, run_perf(item.0), "parse pcap {} unexcepted", item.0);
        }
    }

    fn run_perf(pcap: &str) -> L7PerfStats {
        let rrt_cache = Rc::new(RefCell::new(L7PerfCache::new(100)));
        let mut kafka = KafkaLog::default();

        let capture = Capture::load_pcap(Path::new(FILE_DIR).join(pcap), None);
        let mut packets = capture.as_meta_packets();

        let first_dst_port = packets[0].lookup_key.dst_port;
        for packet in packets.iter_mut() {
            if packet.lookup_key.dst_port == first_dst_port {
                packet.lookup_key.direction = PacketDirection::ClientToServer;
            } else {
                packet.lookup_key.direction = PacketDirection::ServerToClient;
            }

            if packet.get_l4_payload().is_some() {
                let _ = kafka.parse_payload(
                    packet.get_l4_payload().unwrap(),
                    &ParseParam::new(&*packet, rrt_cache.clone(), true, true),
                );
            }
        }
        kafka.perf_stats.unwrap()
    }
}
