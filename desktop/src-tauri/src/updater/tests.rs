use super::*;
use std::{
    collections::VecDeque,
    io::{BufRead, BufReader, Write},
    net::{TcpListener, TcpStream},
    sync::mpsc,
    thread,
};
use tauri::test::{mock_builder, mock_context, noop_assets, MockRuntime};

const LIMIT: Duration = Duration::from_millis(250);
// Public minisign-verify test vector for the payload `test`, never a live key.
const PUBLIC_KEY: &str = "dW50cnVzdGVkIGNvbW1lbnQ6IG1pbmlzaWduIHB1YmxpYyBrZXkgRTc2MjBGMTg0MkI0RTgxRgpSV1FmNkxSQ0dBOWk1M21sWWVjTzRJelQ1MVRHUHB2V3VjTlNDaDFDQk0wUVRhTG43M1k3R0ZPMw==";
const SIGNATURE: &str = "dW50cnVzdGVkIGNvbW1lbnQ6IHNpZ25hdHVyZSBmcm9tIG1pbmlzaWduIHNlY3JldCBrZXkKUlVRZjZMUkNHQTlpNTU5cjNnN1YxcU55SkRBcEdpcDhNZnFjYWRJZ1Q5Q3VoVjNFTWhIb04xbUdUa1VpZEYvejdTcmxRZ1hkeThvZmpiN2JOSkp5bERPb2NyQ284S0x6WndvPQp0cnVzdGVkIGNvbW1lbnQ6IHRpbWVzdGFtcDoxNjMzNzAwODM1CWZpbGU6dGVzdAlwcmVoYXNoZWQKd0xNRGp5OUZMQXV4WjNxNE5sRXZrZ3R5aHJyMGd0VHU2S0M0S0JKZElUYmJPZUFpMXpCSVlvMHY0aVRndDhqSnBJaWRSSm5wOTRBQlFrSkFnQW9vQlE9PQ==";

enum Reply {
    StallHeaders,
    StallBody,
    Feed(&'static str),
    Package(&'static [u8]),
    SlowPackage,
    Current,
}

struct Server {
    url: url::Url,
    stop: mpsc::Sender<()>,
    worker: Option<thread::JoinHandle<usize>>,
    expected: usize,
}

impl Server {
    fn new(replies: Vec<Reply>) -> Self {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        listener.set_nonblocking(true).unwrap();
        let url: url::Url = format!("http://{}/feed", listener.local_addr().unwrap())
            .parse()
            .unwrap();
        let package_url = url.join("/package").unwrap();
        let expected = replies.len();
        let (stop, stopped) = mpsc::channel();
        let worker = thread::spawn(move || {
            let mut replies = VecDeque::from(replies);
            let mut stalled = Vec::new();
            let mut downloads = Vec::new();
            let mut count = 0;
            while stopped.try_recv().is_err() {
                let (mut stream, _) = match listener.accept() {
                    Ok(connection) => connection,
                    Err(error) if error.kind() == std::io::ErrorKind::WouldBlock => {
                        thread::sleep(Duration::from_millis(5));
                        continue;
                    }
                    Err(error) => panic!("local updater server: {error}"),
                };
                stream.set_nonblocking(false).unwrap();
                stream
                    .set_read_timeout(Some(Duration::from_secs(2)))
                    .unwrap();
                let mut request = BufReader::new(&stream);
                loop {
                    let mut line = String::new();
                    assert!(request.read_line(&mut line).unwrap() > 0);
                    if line == "\r\n" {
                        break;
                    }
                }
                count += 1;
                match replies.pop_front().expect("unexpected updater request") {
                    Reply::StallHeaders => stalled.push(stream),
                    Reply::StallBody => {
                        stream
                            .write_all(b"HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nx")
                            .unwrap();
                        stalled.push(stream);
                    }
                    Reply::Feed(version) => {
                        let feed = json!({
                            "version": version,
                            "platforms": {"test-target": {
                                "url": package_url,
                                "signature": SIGNATURE,
                            }},
                        });
                        respond(&mut stream, feed.to_string().as_bytes());
                    }
                    Reply::Package(bytes) => respond(&mut stream, bytes),
                    // A canceled download must not delay the next feed request.
                    Reply::SlowPackage => downloads.push(thread::spawn(move || {
                        if stream
                            .write_all(b"HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\n")
                            .is_err()
                        {
                            return;
                        }
                        for (index, byte) in b"test".iter().enumerate() {
                            if index > 0 {
                                thread::sleep(Duration::from_millis(100));
                            }
                            if stream.write_all(&[*byte]).is_err() {
                                break;
                            }
                        }
                    })),
                    Reply::Current => stream
                        .write_all(b"HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
                        .unwrap(),
                }
            }
            for download in downloads {
                download.join().unwrap();
            }
            count
        });
        Self {
            url,
            stop,
            worker: Some(worker),
            expected,
        }
    }
}

fn respond(stream: &mut TcpStream, body: &[u8]) {
    write!(
        stream,
        "HTTP/1.1 200 OK\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
        body.len()
    )
    .unwrap();
    stream.write_all(body).unwrap();
}

impl Drop for Server {
    fn drop(&mut self) {
        let _ = self.stop.send(());
        let result = self.worker.take().unwrap().join();
        if !thread::panicking() {
            assert_eq!(result.unwrap(), self.expected);
        }
    }
}

fn app() -> tauri::App<MockRuntime> {
    let mut context = mock_context(noop_assets());
    context.config_mut().plugins.0.insert(
        "updater".into(),
        json!({"pubkey": PUBLIC_KEY, "dangerousInsecureTransportProtocol": true}),
    );
    mock_builder()
        .plugin(tauri_plugin_updater::Builder::new().build())
        .manage(Updates::default())
        .manage(Panes::default())
        .build(context)
        .unwrap()
}

async fn refresh(app: &tauri::App<MockRuntime>, server: &Server) -> Status {
    refresh_with_limit(app, server, Duration::from_secs(5)).await
}

async fn refresh_with_limit(
    app: &tauri::App<MockRuntime>,
    server: &Server,
    download_timeout: Duration,
) -> Status {
    let builder = app
        .updater_builder()
        .target("test-target")
        .endpoints(vec![server.url.clone()])
        .unwrap()
        .no_proxy();
    tokio::time::timeout(
        Duration::from_secs(2),
        check_with_timeouts(app.handle(), builder, LIMIT, download_timeout),
    )
    .await
    .expect("updater must settle and release its lock after the request timeout")
}

fn ready(version: &str) -> Status {
    Status::Ready {
        version: version.into(),
    }
}

fn assert_cached(app: &tauri::App<MockRuntime>, version: &str) {
    let updates = app.state::<Updates>();
    assert!(updates.running.try_lock().is_ok(), "updater lock leaked");
    let cached = updates.ready.lock().unwrap();
    let (update, bytes) = cached.as_ref().expect("verified update was lost");
    assert_eq!(update.version, version);
    assert_eq!(bytes, b"test");
    update.verify(bytes).unwrap();
    assert_eq!(updates.status(), ready(version));
}

#[test]
fn stalled_feed_times_out_and_retry_downloads_verified_update() {
    tauri::async_runtime::block_on(async {
        for stall in [Reply::StallHeaders, Reply::StallBody] {
            let app = app();
            let server = Server::new(vec![stall, Reply::Feed("1.0.0"), Reply::Package(b"test")]);
            assert!(matches!(
                refresh(&app, &server).await,
                Status::Failed { .. }
            ));
            assert!(app.state::<Updates>().running.try_lock().is_ok());
            assert_eq!(refresh(&app, &server).await, ready("1.0.0"));
            assert_cached(&app, "1.0.0");
        }
    });
}

#[test]
fn stalled_package_times_out_and_retry_downloads_verified_update() {
    tauri::async_runtime::block_on(async {
        for stall in [Reply::StallHeaders, Reply::StallBody] {
            let app = app();
            let server = Server::new(vec![
                Reply::Feed("1.0.0"),
                stall,
                Reply::Feed("1.0.0"),
                Reply::Package(b"test"),
            ]);
            assert!(matches!(
                refresh(&app, &server).await,
                Status::Failed { .. }
            ));
            assert!(app.state::<Updates>().ready.lock().unwrap().is_none());
            assert!(app.state::<Updates>().running.try_lock().is_ok());
            assert_eq!(refresh(&app, &server).await, ready("1.0.0"));
            assert_cached(&app, "1.0.0");
        }
    });
}

#[test]
fn progressing_download_can_exceed_the_read_timeout() {
    tauri::async_runtime::block_on(async {
        let app = app();
        let server = Server::new(vec![Reply::Feed("1.0.0"), Reply::SlowPackage]);
        assert_eq!(refresh(&app, &server).await, ready("1.0.0"));
        assert_cached(&app, "1.0.0");
    });
}

#[test]
fn overall_download_timeout_bounds_progress_and_allows_retry() {
    tauri::async_runtime::block_on(async {
        let app = app();
        let server = Server::new(vec![
            Reply::Feed("1.0.0"),
            Reply::SlowPackage,
            Reply::Feed("1.0.0"),
            Reply::Package(b"test"),
        ]);
        assert!(matches!(
            refresh_with_limit(&app, &server, Duration::from_millis(150)).await,
            Status::Failed { .. }
        ));
        assert!(app.state::<Updates>().ready.lock().unwrap().is_none());
        assert!(app.state::<Updates>().running.try_lock().is_ok());
        assert_eq!(refresh(&app, &server).await, ready("1.0.0"));
        assert_cached(&app, "1.0.0");
    });
}

#[test]
fn refresh_timeouts_keep_verified_cache_and_allow_newer_retry() {
    tauri::async_runtime::block_on(async {
        let app = app();
        let server = Server::new(vec![
            Reply::Feed("1.0.0"),
            Reply::Package(b"test"),
            Reply::StallHeaders,
            Reply::Feed("2.0.0"),
            Reply::StallBody,
            Reply::Feed("2.0.0"),
            Reply::Package(b"test"),
        ]);
        assert_eq!(refresh(&app, &server).await, ready("1.0.0"));
        assert_cached(&app, "1.0.0");
        assert_eq!(refresh(&app, &server).await, ready("1.0.0"));
        assert_cached(&app, "1.0.0");
        assert_eq!(refresh(&app, &server).await, ready("1.0.0"));
        assert_cached(&app, "1.0.0");
        assert_eq!(refresh(&app, &server).await, ready("2.0.0"));
        assert_cached(&app, "2.0.0");
    });
}

#[test]
fn signature_failure_preserves_verified_cache() {
    tauri::async_runtime::block_on(async {
        let app = app();
        let server = Server::new(vec![
            Reply::Feed("1.0.0"),
            Reply::Package(b"test"),
            Reply::Feed("2.0.0"),
            Reply::Package(b"tampered"),
        ]);
        assert_eq!(refresh(&app, &server).await, ready("1.0.0"));
        assert_eq!(refresh(&app, &server).await, ready("1.0.0"));
        assert_cached(&app, "1.0.0");
    });
}

#[test]
fn unchanged_release_reuses_cache_and_withdrawn_release_clears_it() {
    tauri::async_runtime::block_on(async {
        let app = app();
        let server = Server::new(vec![
            Reply::Feed("1.0.0"),
            Reply::Package(b"test"),
            Reply::Feed("1.0.0"),
            Reply::Current,
        ]);
        assert_eq!(refresh(&app, &server).await, ready("1.0.0"));
        assert_eq!(refresh(&app, &server).await, ready("1.0.0"));
        assert_cached(&app, "1.0.0");
        assert_eq!(
            refresh(&app, &server).await,
            Status::Current {
                version: app.package_info().version.to_string()
            }
        );
        assert!(app.state::<Updates>().ready.lock().unwrap().is_none());
    });
}
