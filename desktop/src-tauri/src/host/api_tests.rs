use super::*;
use std::{
    io::{BufRead, BufReader, Read, Write},
    net::TcpListener,
    sync::mpsc,
};

/// A one-connection HTTP server: records the request line and body, then
/// writes `response` in `parts` and closes.
fn serve(parts: Vec<String>) -> (Url, mpsc::Receiver<(String, String)>) {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let origin = Url::parse(&format!("http://{}/", listener.local_addr().unwrap())).unwrap();
    let (sent, received) = mpsc::channel();
    std::thread::spawn(move || {
        let (stream, _) = listener.accept().unwrap();
        let mut reader = BufReader::new(stream.try_clone().unwrap());
        let mut request = String::new();
        reader.read_line(&mut request).unwrap();
        let mut length = 0;
        loop {
            let mut header = String::new();
            reader.read_line(&mut header).unwrap();
            if header.trim().is_empty() {
                break;
            }
            if let Some(value) = header.to_ascii_lowercase().strip_prefix("content-length:") {
                length = value.trim().parse().unwrap();
            }
        }
        let mut body = vec![0; length];
        reader.read_exact(&mut body).unwrap();
        sent.send((request.trim().to_string(), String::from_utf8(body).unwrap()))
            .unwrap();
        let mut stream = stream;
        for part in parts {
            stream.write_all(part.as_bytes()).unwrap();
            stream.flush().unwrap();
            std::thread::sleep(Duration::from_millis(20));
        }
    });
    (origin, received)
}

fn runtime() -> tokio::runtime::Runtime {
    tokio::runtime::Builder::new_current_thread()
        .enable_all()
        .build()
        .unwrap()
}

const STATUS: &str = r#"{"repository":"/work/notes","commonDir":"/work/notes/.git","runHere":true,"paused":false,"draining":false,"running":{"task":"digest","name":"Daily digest","runId":"r1","startedAt":"2026-11-22T10:00:00Z","stage":"agent"},"next":{"task":"inbox","name":null,"at":"2026-11-23T05:00:00-05:00"},"attention":2,"lastFailure":{"task":"sync","name":"Sync","at":"2026-11-22T09:00:00Z","message":"push rejected","item":7},"notices":[{"id":"n1","kind":"failed","task":"sync","title":"Sync failed","body":"push rejected"}],"later":"ignored"}"#;

#[test]
fn event_streams_split_across_chunks_yield_whole_events() {
    let mut events = Events::default();
    assert!(events.push(b": hello\r\nevent: sta").is_empty());
    assert!(events.push(b"tus\r\ndata: {\"a\":").is_empty());
    assert_eq!(
        events.push(b"1}\r\n\r\ndata: one\ndata: two\n\nevent: ping\n\n"),
        [
            ("status".to_string(), "{\"a\":1}".to_string()),
            ("message".to_string(), "one\ntwo".to_string())
        ]
    );
    // An event with no data dispatches nothing and its name does not leak.
    assert_eq!(
        events.push(b"data:x\n\n"),
        [("message".to_string(), "x".to_string())]
    );
}

#[test]
fn host_status_reads_the_contract_and_tolerates_new_fields() {
    let status: HostStatus = serde_json::from_str(STATUS).unwrap();
    assert!(status.run_here);
    assert_eq!(status.attention, 2);
    assert_eq!(status.running.as_ref().unwrap().run_id, "r1");
    assert_eq!(status.next.as_ref().unwrap().name, None);
    assert_eq!(
        status.last_failure.as_ref().unwrap().message,
        "push rejected"
    );
    assert_eq!(status.notices[0].id, "n1");
    let sparse: HostStatus = serde_json::from_str(r#"{"runHere":true}"#).unwrap();
    assert_eq!(sparse.running, None);
    assert!(sparse.notices.is_empty());
    assert_eq!(
        label(&Some("Daily digest".into()), "digest"),
        "Daily digest"
    );
    assert_eq!(label(&None, "digest"), "digest");
}

#[test]
fn the_stream_delivers_each_status_until_the_runtime_closes_it() {
    let status = STATUS.replace('\n', "");
    let (origin, request) = serve(vec![
        "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nConnection: close\r\n\r\n".into(),
        format!("event: status\ndata: {}", &status[..40]),
        format!("{}\n\n", &status[40..]),
        ": keep-alive\n\nevent: status\ndata: {\"runHere\":false}\n\n".into(),
    ]);
    let api = Api::new(origin).unwrap();
    let mut seen = vec![];
    let ended = runtime().block_on(api.stream(|status| seen.push(status)));
    assert_eq!(request.recv().unwrap().0, "GET /api/tasks/host HTTP/1.1");
    assert_eq!(seen.len(), 2);
    assert_eq!(seen[0].attention, 2);
    assert!(!seen[1].run_here);
    assert_eq!(ended, "Rhizome closed the task host.");
}

#[test]
fn an_old_runtime_without_the_host_api_is_named() {
    let (origin, _request) = serve(vec![
        "HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\nConnection: close\r\n\r\n".into(),
    ]);
    let ended = runtime().block_on(Api::new(origin).unwrap().stream(|_| panic!("no status")));
    assert!(ended.contains("cannot host scheduled tasks"), "{ended}");
}

#[test]
fn requests_name_their_route_and_read_the_reply() {
    let reply = |body: &str| {
        vec![format!(
            "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
            body.len()
        )]
    };
    let runtime = runtime();
    let (origin, request) = serve(reply(r#"{"running":{"task":"digest","runId":"r1"}}"#));
    let running = runtime
        .block_on(Api::new(origin).unwrap().drain(Duration::from_secs(2)))
        .unwrap();
    assert_eq!(running.unwrap().task, "digest");
    assert_eq!(
        request.recv().unwrap().0,
        "POST /api/tasks/host/drain HTTP/1.1"
    );

    let (origin, request) = serve(reply(r#"{"running":null}"#));
    let api = Api::new(origin).unwrap();
    assert_eq!(
        runtime.block_on(api.drain(Duration::from_secs(2))),
        Ok(None)
    );
    request.recv().unwrap();

    let (origin, request) = serve(reply(r#"{"stopped":true}"#));
    let api = Api::new(origin).unwrap();
    assert_eq!(
        runtime.block_on(api.stop_run(Duration::from_secs(2))),
        Ok(true)
    );
    assert_eq!(request.recv().unwrap().0, "POST /api/tasks/stop HTTP/1.1");

    let (origin, request) = serve(reply("{}"));
    let api = Api::new(origin).unwrap();
    runtime
        .block_on(api.pause("2026-11-23T05:00:00Z", Duration::from_secs(2)))
        .unwrap();
    let (line, body) = request.recv().unwrap();
    assert_eq!(line, "POST /api/tasks/pause HTTP/1.1");
    assert_eq!(body, r#"{"until":"2026-11-23T05:00:00Z"}"#);

    // A qualified task id stays one path segment.
    let (origin, request) = serve(reply("{}"));
    let api = Api::new(origin).unwrap();
    runtime
        .block_on(api.run_now("team/digest", Duration::from_secs(2)))
        .unwrap();
    assert_eq!(
        request.recv().unwrap().0,
        "POST /api/tasks/tasks/team%2Fdigest/run HTTP/1.1"
    );

    let (origin, _request) = serve(vec![
        "HTTP/1.1 409 Conflict\r\nContent-Type: application/json\r\nContent-Length: 18\r\nConnection: close\r\n\r\n{\"error\":\"busy\"}  ".into(),
    ]);
    let api = Api::new(origin).unwrap();
    let refused = runtime
        .block_on(api.run_now("digest", Duration::from_secs(2)))
        .unwrap_err();
    assert!(
        refused.contains("409") && refused.contains("busy"),
        "{refused}"
    );
}

#[test]
fn a_started_pass_counts_as_running() {
    let status: HostStatus = serde_json::from_value(serde_json::json!({
        "dispatching": {"pid": 42, "startedAt": "2026-11-22T12:00:00Z"}
    }))
    .unwrap();
    let active = status
        .active()
        .expect("a pass that has not recorded a run is still active");
    assert_eq!(active.started_at, "2026-11-22T12:00:00Z");
    assert_eq!(label(&active.name, &active.task), "Scheduled tasks");
}
