//! W0 probe for the Linux Wayland backend (see docs/design-docs/linux-display-backends.md).
//!
//! `portal [window|monitor]` opens a RemoteDesktop session combined with a
//! ScreenCast source, connects to EIS and prints what the compositor offers:
//! stream geometry and mapping ids, devices, and absolute-pointer regions. It binds
//! seat capabilities but never starts emulating, so no input is sent.

use std::{collections::HashMap, os::unix::net::UnixStream, time::Duration};

use ashpd::desktop::{
    CreateSessionOptions, PersistMode,
    remote_desktop::{ConnectToEISOptions, DeviceType, RemoteDesktop, SelectDevicesOptions, StartOptions},
    screencast::{CursorMode, Screencast, SelectSourcesOptions, SourceType},
};
use futures_util::StreamExt;
use reis::{PendingRequestResult, ei, tokio::EiEventStream};

#[tokio::main(flavor = "current_thread")]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = std::env::args().collect();
    match args.get(1).map(String::as_str) {
        Some("portal") => portal(args.get(2).map(String::as_str) != Some("monitor")).await,
        _ => {
            eprintln!("usage: linux-wayland-probe portal [window|monitor]");
            std::process::exit(2);
        }
    }
}

async fn portal(window: bool) -> Result<(), Box<dyn std::error::Error>> {
    let remote = RemoteDesktop::new().await?;
    let cast = Screencast::new().await?;
    println!("RemoteDesktop version={} devices={:?}", remote.version(), remote.available_device_types().await?);
    println!("ScreenCast version={} sources={:?}", cast.version(), cast.available_source_types().await?);

    let session = remote.create_session(CreateSessionOptions::default()).await?;
    remote
        .select_devices(
            &session,
            SelectDevicesOptions::default()
                .set_devices(DeviceType::Keyboard | DeviceType::Pointer)
                .set_persist_mode(PersistMode::DoNot),
        )
        .await?;
    let source = if window { SourceType::Window } else { SourceType::Monitor };
    let selected = cast
        .select_sources(
            &session,
            SelectSourcesOptions::default()
                .set_sources(ashpd::enumflags2::BitFlags::from(source))
                .set_cursor_mode(CursorMode::Hidden)
                .set_multiple(false),
        )
        .await;
    println!("select_sources({source:?}) -> {:?}", selected.as_ref().map(|_| "ok"));
    selected?;

    println!("Waiting for the portal dialog ...");
    let started = remote.start(&session, None, StartOptions::default()).await?.response()?;
    println!("granted devices={:?}", started.devices());
    for stream in started.streams() {
        println!(
            "stream node={} id={:?} source={:?} position={:?} size={:?} mapping_id={:?}",
            stream.pipe_wire_node_id(),
            stream.id(),
            stream.source_type(),
            stream.position(),
            stream.size(),
            stream.mapping_id()
        );
    }

    let fd = remote.connect_to_eis(&session, ConnectToEISOptions::default()).await?;
    let context = ei::Context::new(UnixStream::from(fd))?;
    let mut events = EiEventStream::new(context.clone())?;
    let handshake = reis::tokio::ei_handshake(&mut events, "cherry-wayland-probe", ei::handshake::ContextType::Sender).await?;
    println!("EIS handshake ok (protocol interfaces negotiated)");
    let _connection = handshake.connection;

    let mut devices: HashMap<ei::Device, Device> = HashMap::new();
    let mut capabilities = 0u64;
    let collect = async {
        while let Some(result) = events.next().await {
            let PendingRequestResult::Request(event) = result? else { continue };
            match event {
                ei::Event::Connection(_, ei::connection::Event::Ping { ping }) => ping.done(0),
                ei::Event::Seat(_, ei::seat::Event::Name { name }) => println!("seat {name}"),
                ei::Event::Seat(_, ei::seat::Event::Capability { mask, interface }) => {
                    println!("  capability {interface} mask={mask:#x}");
                    capabilities |= mask;
                }
                // Binding asks for devices; it does not emit input.
                ei::Event::Seat(seat, ei::seat::Event::Done) => {
                    println!("  bind {capabilities:#x}");
                    seat.bind(capabilities);
                }
                ei::Event::Seat(_, ei::seat::Event::Device { device }) => {
                    devices.insert(device, Device::default());
                }
                ei::Event::Device(device, event) => {
                    let data = devices.entry(device).or_default();
                    match event {
                        ei::device::Event::Name { name } => data.name = name,
                        ei::device::Event::DeviceType { device_type } => data.kind = format!("{device_type:?}"),
                        ei::device::Event::Interface { object } => data.interfaces.push(object.interface().to_owned()),
                        ei::device::Event::RegionMappingId { mapping_id } => data.pending_mapping = Some(mapping_id),
                        ei::device::Event::Region { offset_x, offset_y, width, hight, scale } => data.regions.push(format!(
                            "{offset_x},{offset_y} {width}x{hight} scale={scale} mapping_id={:?}",
                            data.pending_mapping.take()
                        )),
                        ei::device::Event::Done => {
                            println!("device {:?} type={} interfaces={:?}", data.name, data.kind, data.interfaces);
                            for region in &data.regions {
                                println!("  region {region}");
                            }
                        }
                        _ => {}
                    }
                }
                _ => {}
            }
            context.flush().ok();
        }
        Ok::<(), Box<dyn std::error::Error>>(())
    };
    let _ = tokio::time::timeout(Duration::from_secs(5), collect).await;
    println!("Closing session without emulating any input.");
    session.close().await?;
    Ok(())
}

#[derive(Default)]
struct Device {
    name: String,
    kind: String,
    interfaces: Vec<String>,
    regions: Vec<String>,
    pending_mapping: Option<String>,
}
